package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForRoomWatcher 等到长轮询已经挂在房间上等变化：这之后的变化会立即唤醒它，测试不用靠 sleep 对时序。
func waitForRoomWatcher(t *testing.T, roomId int) {
	t.Helper()
	require.Eventually(t, func() bool {
		gameRoomWatch.mu.Lock()
		defer gameRoomWatch.mu.Unlock()
		return len(gameRoomWatch.waiters[roomId]) > 0
	}, 5*time.Second, time.Millisecond)
}

// receivePoll 取回后台长轮询的响应。
func receivePoll(t *testing.T, polled <-chan *httptest.ResponseRecorder) (string, map[string]any) {
	t.Helper()
	select {
	case recorder := <-polled:
		var response map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response), recorder.Body.String())
		require.Equal(t, true, response["success"], response["message"])
		return recorder.Body.String(), response["data"].(map[string]any)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "long poll did not return")
		return "", nil
	}
}

// rpsMoveBody 是出拳请求：带上玩家看到的房间版本与这一拳出在第几回合。
func rpsMoveBody(version any, round int, choice string) map[string]any {
	return map[string]any{"version": version, "move": map[string]any{"choice": choice, "round": round}}
}

// 两位不同 IP 的玩家押 10 个游戏币打一场石头剪刀布，全程走真实的房间接口。先出拳的一方等对手：对手的长轮询立即拿到
// "对方已出拳"，但看不到出了什么；双方都出了才公开这一回合，平局重来。双方同时出拳，后出的一方带着对手出拳之前的
// 房间版本照样出得了，迟到的上一回合的拳则不会落进下一回合。1:1 之后的第四回合房主出了拳、对手一直不出，过了截止时间
// 房主再来轮询就判对手超时输，房主拿走两份押注减去 5% 抽成。
func TestRpsRoomPlaysStakedMatchOverHttp(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Rps = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 20}
	}))
	host := env.player(50, gameTestIpA)
	guest := env.player(50, gameTestIpB)

	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 10})
	assert.Equal(t, model.GameRoomStatusWaiting, created["status"])
	assert.EqualValues(t, 40, created["balance"])
	roomId := int(created["id"].(float64))
	roomPath := fmt.Sprintf("/api/game/rooms/%d", roomId)

	joined := guest.ok(http.MethodPost, roomPath+"/join", nil)
	assert.Equal(t, model.GameRoomStatusPlaying, joined["status"])
	assert.EqualValues(t, 40, joined["balance"])
	assert.EqualValues(t, 1, joined["seat"])
	assert.EqualValues(t, -1, joined["to_move"], "双方同时出拳")
	state := joined["state"].(map[string]any)
	assert.EqualValues(t, 1, state["round"])
	assert.Equal(t, []any{false, false}, state["ready"])

	// 房主挂着长轮询等对手出拳。
	version := joined["version"]
	polled := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		polled <- host.send(http.MethodGet, fmt.Sprintf("%s?after=%v", roomPath, version), nil)
	}()
	waitForRoomWatcher(t, roomId)
	moved := guest.ok(http.MethodPost, roomPath+"/move", rpsMoveBody(version, 1, "rock"))
	assert.EqualValues(t, 0, moved["to_move"], "对手出了拳，轮到房主")
	assert.Equal(t, "rock", moved["state"].(map[string]any)["choice"], "自己能看到自己出的拳")

	body, hostView := receivePoll(t, polled)
	assert.Equal(t, moved["version"], hostView["version"])
	assert.EqualValues(t, 0, hostView["to_move"])
	hostState := hostView["state"].(map[string]any)
	assert.Equal(t, []any{false, true}, hostState["ready"], "房主知道对手已经出了拳")
	assert.Equal(t, "", hostState["choice"])
	assert.False(t, strings.Contains(body, "rock"), "对手这一回合出的拳不能出现在房主的视图里: %s", body)

	// 同一回合里已经出过拳的一方不能再出，房间不变。
	guest.fails(http.MethodPost, roomPath+"/move", rpsMoveBody(moved["version"], 1, "paper"))

	rounds := []struct {
		host, guest string
		wantWins    []any
	}{
		{host: "scissors", guest: "", wantWins: []any{0.0, 1.0}}, // 对手已经出了石头，石头赢剪刀
		{host: "paper", guest: "rock", wantWins: []any{1.0, 1.0}},
		{host: "rock", guest: "rock", wantWins: []any{1.0, 1.0}}, // 平局重来
	}
	view := moved
	for i, round := range rounds {
		// 对手还没看到房主这一拳：带着出拳之前的版本出拳。
		before := view["version"]
		view = host.ok(http.MethodPost, roomPath+"/move", rpsMoveBody(before, i+1, round.host))
		if round.guest != "" {
			assert.EqualValues(t, 1, view["to_move"], "round %d", i+1)
			view = guest.ok(http.MethodPost, roomPath+"/move", rpsMoveBody(before, i+1, round.guest))
		}
		state := view["state"].(map[string]any)
		assert.EqualValues(t, -1, view["to_move"], "round %d", i+1)
		assert.Len(t, state["rounds"], i+1, "round %d", i+1)
		assert.Equal(t, round.wantWins, state["wins"], "round %d", i+1)
	}
	assert.Equal(t, []any{"rock", "rock"}, view["state"].(map[string]any)["rounds"].([]any)[2].(map[string]any)["choices"])

	// 对手第三回合的拳迟到了：第四回合已经开始，它不能算作第四回合的拳。
	guest.fails(http.MethodPost, roomPath+"/move", rpsMoveBody(view["version"], 3, "paper"))
	latest := guest.ok(http.MethodGet, roomPath, nil)
	assert.Equal(t, view["version"], latest["version"])
	assert.Equal(t, []any{false, false}, latest["state"].(map[string]any)["ready"])

	// 第四回合房主出拳，对手一直不出；把这一步的截止时间拨到过去，房主轮询时结算超时。
	view = host.ok(http.MethodPost, roomPath+"/move", rpsMoveBody(view["version"], 4, "paper"))
	assert.EqualValues(t, 1, view["to_move"])
	require.NoError(t, env.db.Model(&model.GameRoom{}).Where("id = ?", roomId).Update("deadline", common.GetTimestamp()-1).Error)

	finished := host.ok(http.MethodGet, roomPath, nil)
	assert.Equal(t, model.GameRoomStatusFinished, finished["status"])
	assert.Equal(t, model.GameRoomEndTimeout, finished["end_reason"])
	assert.EqualValues(t, host.user.Id, finished["winner_id"])
	assert.EqualValues(t, 1, finished["rake"], "奖池 20 个游戏币的 5%")
	assert.Equal(t, 59, host.balance(), "50 - 10 + 20 - 1")
	assert.Equal(t, 40, guest.balance())

	// 结束后双方看到的都是同一个结果，对手也不能再出拳。
	guestView := guest.ok(http.MethodGet, roomPath, nil)
	assert.Equal(t, model.GameRoomStatusFinished, guestView["status"])
	assert.EqualValues(t, -1, guestView["to_move"], "结束后没有人要走")
	assert.EqualValues(t, 0, guestView["deadline"])
	guest.fails(http.MethodPost, roomPath+"/move", rpsMoveBody(guestView["version"], 4, "rock"))
}

// 另一个节点上的变化不会唤醒本进程里的长轮询，要靠后台定期读库的版本发现：对手的出拳直接写进库、不经过本进程，挂着的
// 长轮询照样在一两秒内拿到新版本的房间。
func TestRoomLongPollSeesChangesFromAnotherNode(t *testing.T) {
	env := newGameTestEnv(t, gameSetting(func(s *operation_setting.GameSetting) {
		s.Rps = operation_setting.PvpRule{Enabled: true, MaxStake: 100, RakePercent: 5, MoveSeconds: 20}
	}))
	host := env.player(50, gameTestIpA)
	guest := env.player(50, gameTestIpB)
	created := host.ok(http.MethodPost, "/api/game/rooms", map[string]any{"game": gameRps, "stake": 10})
	roomId := int(created["id"].(float64))
	roomPath := fmt.Sprintf("/api/game/rooms/%d", roomId)
	joined := guest.ok(http.MethodPost, roomPath+"/join", nil)

	polled := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		polled <- host.send(http.MethodGet, fmt.Sprintf("%s?after=%v", roomPath, joined["version"]), nil)
	}()
	waitForRoomWatcher(t, roomId)
	room, err := model.GetGameRoom(roomId)
	require.NoError(t, err)
	require.NoError(t, model.UpdateGameRoomState(room, room.State, 0, room.Deadline))
	require.Greater(t, float64(room.Version), joined["version"])

	start := time.Now()
	_, view := receivePoll(t, polled)
	assert.EqualValues(t, room.Version, view["version"])
	assert.EqualValues(t, 0, view["to_move"])
	assert.Less(t, time.Since(start), pvpPollTimeout/2, "不是等到长轮询超时才返回")
}
