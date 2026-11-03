"""Real old -> current -> second-start checks, on explicit disposable databases only."""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import urllib.error
import urllib.request
import uuid


def arguments():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--driver', required=True, choices=['sqlite', 'mysql', 'postgres'])
    p.add_argument('--dsn-env')
    p.add_argument('--work-root', required=True)
    p.add_argument('--old', required=True)
    p.add_argument('--new', required=True)
    p.add_argument('--db-helper', required=True)
    p.add_argument('--port', type=int, required=True)
    p.add_argument('--disposable', action='store_true', required=True)
    p.add_argument('--keep-final', action='store_true')
    a = p.parse_args()
    if a.driver != 'sqlite' and (not a.dsn_env or a.dsn_env in ['SQL_DSN', 'LOG_SQL_DSN']):
        p.error('use a dedicated disposable DSN variable')
    return a


class Client:
    def __init__(self, base):
        self.base, self.uid = base, None
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def call(self, method, path, body=None, ok=True):
        req = urllib.request.Request(self.base + path, data=json.dumps(body).encode() if body is not None else None, method=method)
        req.add_header('Content-Type', 'application/json')
        if self.uid:
            req.add_header('New-Api-User', str(self.uid))
        try:
            with self.opener.open(req, timeout=60) as response:
                result = json.loads(response.read())
        except urllib.error.HTTPError as e:
            result = json.loads(e.read())
        if ok and not result.get('success'):
            raise RuntimeError(f'{method} {path}: {result.get("message")}')
        return result.get('data') if ok else result


def require_free_port(port):
    if port < 1024 or port > 65535:
        raise RuntimeError('choose an explicit unprivileged local port')
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        try:
            listener.bind(('127.0.0.1', port))
        except OSError as e:
            raise RuntimeError('local port is already occupied; refusing to contact the existing service') from e


def process_alive(pid):
    result = subprocess.run(['powershell.exe', '-NoProfile', '-Command', f'if(Get-Process -Id {int(pid)} -ErrorAction SilentlyContinue){{exit 0}}else{{exit 1}}'], capture_output=True)
    return result.returncode == 0


def wait_ready(client, pid, expected_version, alive=process_alive, attempts=120, pause=time.sleep):
    for _ in range(attempts):
        if not alive(pid):
            raise RuntimeError('the newly launched server exited before readiness')
        try:
            status = client.call('GET', '/api/status')
        except Exception:
            pause(.25)
            continue
        if status.get('version') != expected_version or not alive(pid):
            raise RuntimeError('readiness belongs to another process or version; refusing all writes')
        return
    raise RuntimeError('server failed readiness: ' + expected_version)


def stop_tracked(processes, work, alive=process_alive, attempts=120, pause=time.sleep):
    pid, path, label = processes[-1]
    path.touch(exist_ok=True)
    for _ in range(attempts):
        if not alive(pid):
            text = (work / (label + '.log')).read_text(encoding='utf-8', errors='replace')
            if 'server exited' not in text:
                raise RuntimeError('server did not complete graceful shutdown: ' + label)
            processes.pop()
            return
        pause(.1)
    raise RuntimeError('server shutdown timed out; process remains tracked: ' + label)


def run(a):
    require_free_port(a.port)
    parent = Path(a.work_root).resolve()
    if 'wendao-site-check-' not in str(parent):
        raise RuntimeError('work-root must be an explicit wendao-site-check- directory')
    parent.mkdir(parents=True, exist_ok=True)
    work = parent / (a.driver + '-' + uuid.uuid4().hex)
    work.mkdir()
    password = 'upgrade-' + uuid.uuid4().hex[:16]
    base = f'http://127.0.0.1:{a.port}'
    processes = []
    checks = []
    sqlite_path = str(work / 'fixture.db')

    def db(mode, raw=None):
        args = [a.db_helper, '-driver', a.driver, '-mode', mode]
        args += ['-sqlite', sqlite_path] if a.driver == 'sqlite' else ['-dsn-env', a.dsn_env]
        result = subprocess.run(args, input=json.dumps(raw) if raw is not None else None, capture_output=True, text=True, encoding='utf-8')
        if result.returncode:
            raise RuntimeError(f'database helper {mode} failed (DSN omitted): {result.stderr[:160]}')
        return result.stdout

    def saved():
        return json.loads(db('save'))

    def start(binary, label):
        require_free_port(a.port)
        stop = work / (label + '.stop')
        version_label = label + '-' + uuid.uuid4().hex
        env = dict(os.environ)
        for key in list(env):
            if key.startswith(('SQL_', 'LOG_SQL_', 'REDIS_', 'SESSION_', 'GAME_', 'WALLET_', 'TRUSTED_', 'HTTP_', 'HTTPS_', 'ALL_PROXY', 'UMAMI_', 'GOOGLE_', 'CRYPTO_')):
                env.pop(key, None)
        env.update({'SQL_DSN': '' if a.driver == 'sqlite' else os.environ[a.dsn_env], 'SQLITE_PATH': sqlite_path,
                    'SESSION_SECRET': 'fixture-' + uuid.uuid4().hex, 'REDIS_CONN_STRING': '', 'LOG_SQL_DSN': '',
                    'WENDAO_CHECK_STOP_FILE': str(stop), 'PORT': str(a.port), 'GIN_MODE': 'release',
                    'GLOBAL_API_RATE_LIMIT_ENABLE': 'false', 'CRITICAL_RATE_LIMIT_ENABLE': 'false',
                    'VERSION': version_label, 'NO_PROXY': '127.0.0.1,localhost', 'SHUTDOWN_TIMEOUT_SECONDS': '5'})
        # PowerShell Start-Process hides the Windows child; no visible console is opened.
        launcher = work / (label + '.ps1')
        escaped = lambda value: "'" + str(value).replace("'", "''") + "'"
        command = "$p=Start-Process -WindowStyle Hidden -FilePath " + escaped(Path(binary).resolve())
        command += ' -WorkingDirectory ' + escaped(work) + ' -PassThru -RedirectStandardOutput ' + escaped(work/(label+'.log'))
        pid_file = work / (label + '.pid')
        command += ' -RedirectStandardError ' + escaped(work/(label+'.err.log')) + '; Set-Content -LiteralPath ' + escaped(pid_file) + ' -Value $p.Id'
        launcher.write_text(command, encoding='utf-8')
        launched = subprocess.run(['powershell.exe', '-NoProfile', '-File', str(launcher)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if launched.returncode:
            raise RuntimeError('hidden server launch failed')
        pid = int(pid_file.read_text().strip())
        processes.append((pid, stop, label))
        client = Client(base)
        wait_ready(client, pid, version_label)
        return client

    def stop_current():
        stop_tracked(processes, work)

    def login(c):
        c.uid = c.call('POST', '/api/user/login', {'username': 'root', 'password': password})['id']
        return c

    try:
        db('empty')
        version = db('version').strip()
        c = start(a.old, 'v2.16.28-fixture')
        c.call('POST', '/api/setup', {'username':'root','password':password,'confirmPassword':password,'SelfUseModeEnabled':False,'DemoSiteEnabled':False})
        login(c)
        c.call('PUT', '/api/game/admin/setting', {'enabled':True,'play_turnstile_enabled':False,
              'wendao':{'enabled':True},'wendao_tournament':{'enabled':True,'daily_rewards':0}})
        c.call('POST','/api/game/wendao/hero/action',{'action':'create','class':'mage'})
        c.call('POST','/api/game/wendao/hero/action',{'action':'visit','node':'c1_oath'})
        raw=saved()
        assert raw['schema']==7
        raw['hero']['realm']=2
        raw['sect']['res']={'stone':50000,'herb':50000,'ore':50000,'rep':1000}
        db('write',raw)
        c.call('POST','/api/game/wendao/sect/path',{'action':'found'})
        sect=c.call('POST','/api/game/wendao/sect/action',{'action':'sync'})['sect']
        team=[sect['disciples'][0]['id']]
        c.call('POST','/api/game/wendao/sect/action',{'action':'plant','index':0,'crop':'qingcao'})
        c.call('POST','/api/game/wendao/hero/action',{'action':'room','item':'quiet'})
        tour=c.call('POST','/api/game/wendao/sect/tournament/start',{'team':team})
        rift=c.call('POST','/api/game/wendao/rift/start',{'class':'mage'})['session']
        rift=c.call('POST','/api/game/wendao/rift/act',{'action':'enter','choice':0,'session_id':rift['session_id'],'version':rift['version']})['session']
        c.call('POST','/api/game/wendao/hero/action',{'action':'fight','node':'c1_backhill'})
        legacy=saved()
        assert legacy['hero']['battle']['run']
        stop_current()
        checks.append('old_site_setup_settings_hero_cave_crop_and_three_active_modes')

        c=login(start(a.new,'current-first'))
        settings=c.call('GET','/api/game/admin/setting')['setting']
        assert settings['enabled'] and settings['wendao']['enabled'] and settings['wendao_tournament']['enabled']
        hero=c.call('GET','/api/game/wendao/hero')['hero']
        assert hero['class']=='mage' and not hero.get('battle') and hero['cave']['quiet']==1
        assert hero['nodes']['c1_oath']['clears']==1
        c.call('POST','/api/game/wendao/hero/action',{'action':'sync','request_id':str(uuid.uuid4())})
        upgraded=saved()
        assert upgraded['schema']==13 and upgraded['hero']['cards']==legacy['hero']['cards']
        expected_potions=dict(legacy['hero'].get('potions',{}))
        for potion in legacy['hero']['battle']['run'].get('potions',[]):
            expected_potions[potion]=min(expected_potions.get(potion,0)+1,9)
        assert upgraded['hero']['potions']==expected_potions
        sect=c.call('POST','/api/game/wendao/sect/action',{'action':'sync'})['sect']
        assert sect['garden']['plots'][0]['crop']=='qingcao'
        current_tour=c.call('GET','/api/game/wendao/sect/tournament')['session']
        assert current_tour['session_id']==tour['session_id']
        c.call('POST','/api/game/wendao/sect/tournament/action',{'action':'abandon','session_id':current_tour['session_id'],'version':current_tour['version']})
        current_rift=c.call('GET','/api/game/wendao/rift/state')['session']
        assert current_rift['session_id']==rift['session_id'] and current_rift['run']['room']['hidden']
        c.call('POST','/api/game/wendao/rift/act',{'action':'abandon','session_id':current_rift['session_id'],'version':current_rift['version']})
        fought=c.call('POST','/api/game/wendao/hero/action',{'action':'fight','node':'c1_backhill'})
        c.call('POST','/api/game/wendao/hero/action',{'action':'flee','battle_token':fought['hero']['battle']['token']})
        checks.append('current_site_settings_and_legacy_progress_preserved_old_card_fight_closed_and_sessions_resumed')
        stable=saved()
        stop_current()
        c=login(start(a.new,'current-second'))
        c.call('GET','/api/game/wendao/hero')
        c.call('GET','/api/game/wendao/sect/outpost')
        c.call('GET','/api/game/wendao/sect/lineage')
        after=saved()
        assert after==stable
        stop_current()
        checks.append('second_full_site_start_and_new_management_readers_preserve_save')

        future=dict(stable); future['schema']=999; future['future_progress']={'retain':12345}
        db('write',future)
        bytes_before=db('save')
        c=login(start(a.new,'current-future'))
        for rejected in [c.call('GET','/api/game/wendao/hero',ok=False),c.call('POST','/api/game/wendao/hero/action',{'action':'sync'},ok=False)]:
            assert not rejected['success'] and ('newer version' in rejected.get('message','') or '更新版本' in rejected.get('message',''))
        assert db('save')==bytes_before
        stop_current()
        db('write',stable)
        checks.append('future_schema_read_and_write_refused_without_overwrite')

        bytes_before=db('save')
        c=login(start(a.old,'old-refusal'))
        for rejected in [c.call('GET','/api/game/wendao/hero',ok=False),c.call('POST','/api/game/wendao/hero/action',{'action':'sync'},ok=False)]:
            assert not rejected['success'] and ('newer version' in rejected.get('message','') or '更新版本' in rejected.get('message',''))
        assert db('save')==bytes_before
        stop_current()
        checks.append('old_binary_refuses_current_schema_without_overwrite')

        c=login(start(a.new,'current-final'))
        c.call('GET','/api/game/wendao/hero')
        report={'driver':a.driver,'engine_version':version,'old':'v2.16.28 ae847d15b schema7','new_schema':13,
                'scope':'real full site startup and HTTP upgrade checks; fixture promotion is explicit, not a gameplay balance proof',
                'checks':checks,'tables':int(db('counts')),'base':base,'work':str(work),
                'server_running':a.keep_final,'binary_overlay':'loopback bind and stop-file sending SIGTERM to existing graceful shutdown; business code unchanged'}
        if not a.keep_final:
            stop_current()
        else:
            report['temporary_login']={'username':'root','password':password}
            report['pid']=processes[-1][0]
            report['stop_file']=str(processes[-1][1])
            processes.clear()
        (work/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2),encoding='utf-8')
        print(json.dumps(report,ensure_ascii=False))
    finally:
        # 一次清理超时保留跟踪和停止文件，由执行者按 PID/日志继续处理，不机械重试或强杀。
        if processes:
            stop_current()


if __name__=='__main__':
    run(arguments())
