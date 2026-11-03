package doufa

import (
	_ "embed"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// contentJSON 是斗法的全部数值与房间模板，只有这一份。前端读的是它的复制件
// web/classic/src/pages/Games/wendao/doufa/content.json，TestBrowserContentIsCurrent 核对两份一字不差。
//
// 长度单位是像素、速度是像素每秒、时间是帧(60 帧一秒)，规则里用的时候才换成整数单位(Scale)。
//
//go:embed content.json
var contentJSON []byte

// ShotSpec 是一发(或一排)飞弹：Count 发、相邻两发差 Spread 个方向(64 个方向一圈)。
type ShotSpec struct {
	Damage int `json:"damage"`
	Speed  int `json:"speed"`
	Radius int `json:"radius"`
	Count  int `json:"count"`
	Spread int `json:"spread"`
}

// BossSpec 是首领的几套招式：喷射(对着主角的扇形)、弹环、召唤小怪、冲撞，依次轮换，Gap 是两套之间走动的帧数；
// 生命掉到一半以后进入第二阶段，用 Count2、Gap2。
type BossSpec struct {
	Gap   int `json:"gap"`
	Gap2  int `json:"gap2"`
	Spray struct {
		Volleys int `json:"volleys"`
		Every   int `json:"every"`
		Count   int `json:"count"`
		Count2  int `json:"count2"`
		Spread  int `json:"spread"`
		Damage  int `json:"damage"`
		Speed   int `json:"speed"`
		Radius  int `json:"radius"`
	} `json:"spray"`
	Ring struct {
		Volleys int `json:"volleys"`
		Every   int `json:"every"`
		Count   int `json:"count"`
		Count2  int `json:"count2"`
		Turn    int `json:"turn"`
		Damage  int `json:"damage"`
		Speed   int `json:"speed"`
		Radius  int `json:"radius"`
	} `json:"ring"`
	Summon struct {
		Kind  string `json:"kind"`
		Count int    `json:"count"`
		Cap   int    `json:"cap"`
	} `json:"summon"`
	Charge struct {
		Windup int `json:"windup"`
		Dash   int `json:"dash"`
		Ticks  int `json:"ticks"`
	} `json:"charge"`
}

// FoeSpec 是一种敌人。AI 是行为：chase 追身、shoot 保持距离放弹、charge 蓄力冲撞、turret 原地放弹环、boss 首领。
type FoeSpec struct {
	AI        string    `json:"ai"`
	HP        int       `json:"hp"`
	Radius    int       `json:"radius"`
	Speed     int       `json:"speed"`
	Touch     int       `json:"touch"`
	Range     int       `json:"range"`
	Cooldown  int       `json:"cooldown"`
	Windup    int       `json:"windup"`
	Dash      int       `json:"dash"`
	DashTicks int       `json:"dash_ticks"`
	Rest      int       `json:"rest"`
	Spin      int       `json:"spin"`
	Shot      ShotSpec  `json:"shot"`
	Boss      *BossSpec `json:"boss"`
}

// WeaponSpec 是一件法宝。Kind 为 melee 时挥砍扇形(Range 像素、左右各 Arc 个方向)，为 shot 时放飞弹并耗 Cost 点灵力。
type WeaponSpec struct {
	Kind     string `json:"kind"`
	Damage   int    `json:"damage"`
	Cooldown int    `json:"cooldown"`
	Range    int    `json:"range"`
	Arc      int    `json:"arc"`
	Knock    int    `json:"knock"`
	Speed    int    `json:"speed"`
	Radius   int    `json:"radius"`
	Pierce   int    `json:"pierce"`
	Cost     int    `json:"cost"`
	Count    int    `json:"count"`
	Spread   int    `json:"spread"`
	Life     int    `json:"life"`
	Splash   int    `json:"splash"`
}

// SkillSpec 是流派法术：quake 震地(身边一圈伤害并击退)、nova 焚天(向四周放一圈火球)、blades 剑阵(飞剑绕身旋转)。
type SkillSpec struct {
	Kind     string `json:"kind"`
	Cost     int    `json:"cost"`
	Cooldown int    `json:"cooldown"`
	Damage   int    `json:"damage"`
	Radius   int    `json:"radius"`
	Knock    int    `json:"knock"`
	Count    int    `json:"count"`
	Speed    int    `json:"speed"`
	Life     int    `json:"life"`
	Ticks    int    `json:"ticks"`
	Orbit    int    `json:"orbit"`
	Rehit    int    `json:"rehit"`
}

// ClassSpec 是一个流派：生命、灵力、移速、起手的两件法宝与法术。
type ClassSpec struct {
	HP      int       `json:"hp"`
	MP      int       `json:"mp"`
	Speed   int       `json:"speed"`
	Weapons []string  `json:"weapons"`
	Skill   SkillSpec `json:"skill"`
}

// Group 是一波里的一种敌人与数目。
type Group struct {
	Foe string `json:"foe"`
	N   int    `json:"n"`
}

// Blessing 是秘境斗法里的一种祝福：这一轮里一直生效的加成，每种只能拿一次。
type Blessing struct {
	Id   string `json:"id"`
	Mods Mods   `json:"mods"`
}

// Talent 是一种道韵天赋：用道韵升级，Costs[i] 是升到第 i+1 级要花的道韵。每升一级给下面这些东西，N 级就是 N 倍：
// Mods 是整轮生效的加成(与祝福叠在一起)，Retreats 是多几次退路，Rest 是静修多回生命上限的百分之几，Blessings 是开局
// 多送几样随机祝福。天赋只在开局时生效，一轮开始以后买的天赋下一轮才有。
type Talent struct {
	Id        string `json:"id"`
	Costs     []int  `json:"costs"`
	Mods      Mods   `json:"mods"`
	Retreats  int    `json:"retreats,omitempty"`
	Rest      int    `json:"rest,omitempty"`
	Blessings int    `json:"blessings,omitempty"`
}

// Content 是 content.json 的全部内容。Blessings、Talents 与 WeaponPool 是数组，按固定的先后抽取或排列，两边一致。
type Content struct {
	Version int `json:"version"`
	Player  struct {
		Radius  int `json:"radius"`
		MPRegen int `json:"mp_regen"`
		Iframes int `json:"iframes"`
		Pickup  int `json:"pickup"`
		Dodge   struct {
			Speed    int `json:"speed"`
			Ticks    int `json:"ticks"`
			Cooldown int `json:"cooldown"`
		} `json:"dodge"`
	} `json:"player"`
	Classes    map[string]ClassSpec  `json:"classes"`
	Weapons    map[string]WeaponSpec `json:"weapons"`
	Foes       map[string]FoeSpec    `json:"foes"`
	Rooms      map[string][]string   `json:"rooms"`
	Blessings  []Blessing            `json:"blessings"`
	Talents    []Talent              `json:"talents"`
	WeaponPool []string              `json:"weapon_pool"`
	Trial      struct {
		Room  string    `json:"room"`
		Waves [][]Group `json:"waves"`
	} `json:"trial"`
}

// content 是读好的数值；读不出来或自相矛盾时程序起不来(测试会先发现)。
var content = mustLoadContent()

func mustLoadContent() *Content {
	var c Content
	if err := common.Unmarshal(contentJSON, &c); err != nil {
		panic(fmt.Sprintf("doufa: content.json: %v", err))
	}
	return &c
}

// TrialSetup 是试剑台的那一间房：流派起手的法宝，四波敌人，最后一波是首领。
func TrialSetup(class string) Setup {
	setup := Setup{Class: class, Room: content.Trial.Room, Waves: content.Trial.Waves}
	if spec, ok := content.Classes[class]; ok {
		setup.Weapons = spec.Weapons
	}
	return setup
}

// StartingWeapons 是流派起手的两件法宝(新切片)，不认识的流派返回 nil。
func StartingWeapons(class string) []string {
	spec, ok := content.Classes[class]
	if !ok {
		return nil
	}
	return append([]string(nil), spec.Weapons...)
}
