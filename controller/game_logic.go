package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/bulls"
	"github.com/QuantumNous/new-api/pkg/hanoi"
	"github.com/QuantumNous/new-api/pkg/lightsout"
	"github.com/QuantumNous/new-api/pkg/match3"
	"github.com/QuantumNous/new-api/pkg/nonogram"
	"github.com/QuantumNous/new-api/pkg/onestroke"
	"github.com/QuantumNous/new-api/pkg/twentyfour"
	"github.com/QuantumNous/new-api/pkg/watersort"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// logicPuzzleState 只保存难度与合法操作，局面从会话种子重建；撤销和重置不相信客户端传来的盘面。Inputs 是格子、走法这类
// 数字操作(熄灯、汉诺塔、数织、倒水排序、一笔画、宝石消除)，Answers 是文字操作(24 点的算式与跳过、猜数字的每一次猜测)。
type logicPuzzleState struct {
	Difficulty int      `json:"difficulty"`
	Inputs     []int    `json:"inputs"`
	Answers    []string `json:"answers,omitempty"`
}

// logicPuzzle 是一款按步保存的解谜游戏：写进审计与流水的名字、能不能撤销重置、每一步最多算几秒有效游玩时长。
// 24 点和猜数字的一步要想很久，按步封顶的时长放宽；它们也不能撤销，否则可以把猜错的一次撤回再猜。宝石消除补上来的宝石
// 来自同一条随机数序列，撤销后重走能看到下一批宝石，所以也不能撤销。
type logicPuzzle struct {
	name           string
	undoable       bool
	secondsPerStep int
}

var logicPuzzles = map[string]logicPuzzle{
	"lightsout":  {name: "熄灯挑战", undoable: true, secondsPerStep: 5},
	"hanoi":      {name: "汉诺塔", undoable: true, secondsPerStep: 5},
	"nonogram":   {name: "数织", undoable: true, secondsPerStep: 5},
	"twentyfour": {name: "24 点", secondsPerStep: 60},
	"bulls":      {name: "猜数字", secondsPerStep: 40},
	"watersort":  {name: "倒水排序", undoable: true, secondsPerStep: 6},
	"onestroke":  {name: "一笔画", undoable: true, secondsPerStep: 6},
	"match3":     {name: "宝石消除", secondsPerStep: 20},
}

// onestrokeMaxBatch 是一笔画一次请求最多连走几格：页面在手指划过时先画在本地，松手后把这一段一起提交。
const onestrokeMaxBatch = onestroke.MaxSize * onestroke.MaxSize

// twentyfourSkip 是 Answers 里表示跳过一题的记号，算式里不会出现。
const twentyfourSkip = "skip"

func init() {
	for _, key := range []string{"lightsout", "hanoi", "nonogram", "twentyfour", "bulls", "watersort", "onestroke", "match3"} {
		registerGame(&gameDefinition{Key: key, Kind: operation_setting.GameKindSkill,
			Info: func(ctx *gameContext) (gin.H, error) {
				rule, _ := ctx.Setting.SkillRuleOf(key)
				info, err := skillInfo(ctx, key, rule)
				if err == nil && key == "match3" {
					// 各难度的颜色数、步数与目标分，页面选难度时列出来。
					info["levels"] = match3.Levels
				}
				return info, err
			},
			Routes: func(group *gin.RouterGroup) {
				group.GET("/state", func(c *gin.Context) { getLogicPuzzle(c, key) })
				group.POST("/start", middleware.GamePlayRateLimit(key+"-start"), func(c *gin.Context) { startLogicPuzzle(c, key) })
				group.POST("/action", middleware.GameActionRateLimit(key+"-action"), func(c *gin.Context) { actLogicPuzzle(c, key) })
			},
		})
	}
}

// logicPuzzleView 用规则引擎重放服务端保存的操作，所有盘面信息都公开，两种玩法共用存档、版本和领奖流程。
func logicPuzzleView(key string, session *model.GameSession, state logicPuzzleState) (gin.H, error) {
	view := gin.H{"session_id": session.Id, "version": session.Version, "difficulty": state.Difficulty,
		"moves": len(state.Inputs) + len(state.Answers), "status": session.Status, "won": false, "over": false}
	switch key {
	case "lightsout":
		game, err := lightsout.New(state.Difficulty, uint64(session.Seed))
		if err != nil || len(state.Inputs) > lightsout.MaxMoves {
			return nil, lightsout.ErrMove
		}
		for _, cell := range state.Inputs {
			if err := game.Move(cell); err != nil {
				return nil, err
			}
		}
		view["board"], view["size"], view["won"], view["max_moves"] = game.Board, game.Size, game.Solved(), lightsout.MaxMoves
	case "hanoi":
		game, err := hanoi.New(state.Difficulty)
		if err != nil || len(state.Inputs) > hanoi.MaxMoves {
			return nil, hanoi.ErrMove
		}
		for _, move := range state.Inputs {
			if move < 0 || move > 8 {
				return nil, hanoi.ErrMove
			}
			if err := game.Move(move/3, move%3); err != nil {
				return nil, err
			}
		}
		view["towers"], view["disks"], view["won"] = game.Towers, game.Disks, game.Solved()
		view["minimum_moves"], view["max_moves"] = game.MinimumMoves(), hanoi.MaxMoves
	case "nonogram":
		game, err := nonogram.New(state.Difficulty, uint64(session.Seed))
		if err != nil || len(state.Inputs) > nonogram.MaxMoves {
			return nil, nonogram.ErrMove
		}
		for _, cell := range state.Inputs {
			if err := game.Toggle(cell); err != nil {
				return nil, err
			}
		}
		view["size"], view["rows"], view["cols"] = game.Size, game.Rows, game.Cols
		view["filled"], view["won"], view["max_moves"] = game.Filled, game.Solved(), nonogram.MaxMoves
	case "watersort":
		game, err := watersort.New(state.Difficulty, uint64(session.Seed))
		if err != nil || len(state.Inputs) > watersort.MaxMoves {
			return nil, watersort.ErrMove
		}
		// last 是最后一次倒水，页面据此播放倾倒的动画。
		var last gin.H
		for _, move := range state.Inputs {
			amount, err := game.Pour(move/16, move%16)
			if err != nil {
				return nil, err
			}
			last = gin.H{"from": move / 16, "to": move % 16, "amount": amount}
		}
		view["tubes"], view["capacity"], view["won"], view["last"] = game.Tubes, watersort.Capacity, game.Solved(), last
		view["stuck"], view["max_moves"] = !game.Solved() && !game.HasMove(), watersort.MaxMoves
	case "onestroke":
		game, err := onestroke.New(state.Difficulty, uint64(session.Seed))
		if err != nil {
			return nil, err
		}
		for _, cell := range state.Inputs {
			if err := game.Step(cell); err != nil {
				return nil, err
			}
		}
		view["size"], view["stones"], view["path"], view["open"] = game.Size, game.Stones, game.Path, game.Open()
		view["won"], view["stuck"] = game.Solved(), game.Stuck()
	case "match3":
		game, err := match3.New(state.Difficulty, uint64(session.Seed))
		if err != nil {
			return nil, err
		}
		for _, move := range state.Inputs {
			if err := game.Swap(move/2, move%2); err != nil {
				return nil, err
			}
		}
		level := match3.Levels[state.Difficulty]
		view["board"], view["score"], view["target"], view["moves_left"] = game.Board, game.Score, level.Target, game.MovesLeft()
		view["move_limit"], view["phases"], view["shuffled"] = level.Moves, game.Phases, game.Shuffled
		view["won"], view["over"] = game.Won(), game.Over()
		if n := len(state.Inputs); n > 0 {
			view["swap"] = gin.H{"cell": state.Inputs[n-1] / 2, "dir": state.Inputs[n-1] % 2}
		}
	case "twentyfour":
		game, err := twentyfour.New(state.Difficulty, uint64(session.Seed))
		if err != nil {
			return nil, err
		}
		// 已经答过或跳过的题按顺序列出来；跳过的题附上一种解法。
		history := make([]gin.H, 0, len(state.Answers))
		for _, answer := range state.Answers {
			hand := game.Current()
			if answer == twentyfourSkip {
				err = game.Skip()
				history = append(history, gin.H{"hand": hand, "skipped": true, "answer": twentyfour.Solve(hand)})
			} else {
				err = game.Submit(answer)
				history = append(history, gin.H{"hand": hand, "answer": answer})
			}
			if err != nil {
				return nil, err
			}
		}
		view["history"], view["solved"], view["skips_left"] = history, game.Solved, twentyfour.MaxSkips-game.Skips
		view["hands_per_round"], view["won"] = twentyfour.HandsPerRound, game.Won()
		if !game.Won() {
			view["hand"] = game.Current()
		}
	case "bulls":
		game, err := bulls.New(state.Difficulty, uint64(session.Seed))
		if err != nil {
			return nil, err
		}
		for _, guess := range state.Answers {
			if _, err := game.Guess(guess); err != nil {
				return nil, err
			}
		}
		level := bulls.Levels[state.Difficulty]
		history := game.History
		if history == nil {
			history = []bulls.Feedback{}
		}
		view["digits"], view["max_guesses"], view["history"] = level.Digits, level.Guesses, history
		view["candidates"], view["won"], view["over"] = game.Candidates(), game.Won(), game.Over()
		// 答案只在这一局结束(猜中、次数用完或主动结束)之后公开。
		if game.Over() || session.Status == model.GameSessionStatusFinished {
			view["secret"] = game.Secret
		}
	default:
		return nil, errors.New("unknown logic puzzle")
	}
	return view, nil
}

func startLogicPuzzle(c *gin.Context, key string) {
	if !requireGameEnabled(c, key) {
		return
	}
	var req struct {
		Difficulty int `json:"difficulty"`
	}
	if decodeGameRequest(c, &req) != nil || req.Difficulty < 1 || req.Difficulty > 3 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	state := logicPuzzleState{Difficulty: req.Difficulty, Inputs: []int{}}
	session := startSkillSession(c, key, func(uint32) (string, error) {
		data, err := common.Marshal(state)
		return string(data), err
	})
	if session == nil {
		return
	}
	view, err := logicPuzzleView(key, session, state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func getLogicPuzzle(c *gin.Context, key string) {
	if !requireGameEnabled(c, key) {
		return
	}
	var session *model.GameSession
	var err error
	if requested := c.Query("session_id"); requested != "" {
		id, parseErr := strconv.Atoi(requested)
		if parseErr != nil || id <= 0 {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		session, err = model.GetUserGameSession(c.GetInt("id"), key, id)
	} else {
		session, err = model.GetPlayingGameSession(c.GetInt("id"), key)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session == nil || common.GetTimestamp()-session.CreatedAt > skillSessionTTL {
		common.ApiSuccess(c, gin.H{"session": nil})
		return
	}
	var state logicPuzzleState
	if err := common.UnmarshalJsonStr(session.State, &state); err != nil {
		common.ApiError(c, err)
		return
	}
	view, err := logicPuzzleView(key, session, state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if session.Status == model.GameSessionStatusFinished {
		rule, _ := operation_setting.GetGameSetting().SkillRuleOf(key)
		result, err := skillSettledView(c, session, rule)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		view["result"] = result
	}
	common.ApiSuccess(c, gin.H{"session": view})
}

func actLogicPuzzle(c *gin.Context, key string) {
	if !requireGameEnabled(c, key) {
		return
	}
	var req struct {
		SessionId int    `json:"session_id"`
		Version   int    `json:"version"`
		Action    string `json:"action"`
		Cell      *int   `json:"cell"`
		From      *int   `json:"from"`
		To        *int   `json:"to"`
		// Cells 是一笔画一次连走的几格，Dir 是宝石消除交换的方向(0 右边、1 下面)，Steps 是一次撤销几步(默认一步)。
		Cells []int `json:"cells"`
		Dir   *int  `json:"dir"`
		Steps *int  `json:"steps"`
		// Answer 是 24 点的算式或猜数字的一次猜测。
		Answer string `json:"answer"`
	}
	if decodeGameRequest(c, &req) != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	rule, _ := operation_setting.GetGameSetting().SkillRuleOf(key)
	session := loadSkillSession(c, key, rule, req.SessionId)
	if session == nil {
		return
	}
	if session.Version != req.Version {
		common.ApiErrorI18n(c, i18n.MsgGameRoundChanged)
		return
	}
	var state logicPuzzleState
	if err := common.UnmarshalJsonStr(session.State, &state); err != nil {
		common.ApiError(c, err)
		return
	}
	puzzle := logicPuzzles[key]
	switch req.Action {
	case "move":
		switch key {
		case "lightsout", "nonogram":
			if req.Cell == nil {
				common.ApiErrorI18n(c, i18n.MsgInvalidParams)
				return
			}
			state.Inputs = append(state.Inputs, *req.Cell)
		case "hanoi":
			if req.From == nil || req.To == nil || *req.From < 0 || *req.From > 2 || *req.To < 0 || *req.To > 2 {
				common.ApiErrorI18n(c, i18n.MsgInvalidParams)
				return
			}
			state.Inputs = append(state.Inputs, *req.From*3+*req.To)
		case "watersort":
			// 试管编号由规则引擎核对，这里只挡住会编码越界的数字。
			if req.From == nil || req.To == nil || *req.From < 0 || *req.From > 15 || *req.To < 0 || *req.To > 15 {
				common.ApiErrorI18n(c, i18n.MsgInvalidParams)
				return
			}
			state.Inputs = append(state.Inputs, *req.From*16+*req.To)
		case "onestroke":
			if len(req.Cells) == 0 || len(req.Cells) > onestrokeMaxBatch {
				common.ApiErrorI18n(c, i18n.MsgInvalidParams)
				return
			}
			state.Inputs = append(state.Inputs, req.Cells...)
		case "match3":
			if req.Cell == nil || req.Dir == nil || *req.Cell < 0 || *req.Cell >= match3.Cells || *req.Dir < 0 || *req.Dir > 1 {
				common.ApiErrorI18n(c, i18n.MsgInvalidParams)
				return
			}
			state.Inputs = append(state.Inputs, *req.Cell*2+*req.Dir)
		default:
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
	case "answer":
		// 算式与猜测都很短，过长的直接拒绝，不进规则引擎。
		if (key != "twentyfour" && key != "bulls") || req.Answer == "" || req.Answer == twentyfourSkip || len(req.Answer) > twentyfour.MaxExpressionLength {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		state.Answers = append(state.Answers, req.Answer)
	case "skip":
		if key != "twentyfour" {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		state.Answers = append(state.Answers, twentyfourSkip)
	case "undo":
		if !puzzle.undoable {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		steps := 1
		if req.Steps != nil {
			steps = *req.Steps
		}
		if steps < 1 || steps > len(state.Inputs) {
			common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
			return
		}
		state.Inputs = state.Inputs[:len(state.Inputs)-steps]
	case "reset":
		if !puzzle.undoable {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		state.Inputs = []int{}
	case "finish":
	default:
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	steps := len(state.Inputs) + len(state.Answers)
	elapsed := max(int64(0), common.GetTimestamp()-session.CreatedAt)
	if int64(steps) > (elapsed+2)*8 {
		common.ApiErrorI18n(c, i18n.MsgGameReplayInvalid)
		return
	}
	view, err := logicPuzzleView(key, session, state)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgGameMoveInvalid)
		return
	}
	data, err := common.Marshal(state)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	won := view["won"].(bool)
	// 猜数字用完次数、宝石消除用完步数也结束：成绩为 0，按已经玩过的时长结算。
	over := won || view["over"].(bool)
	if over || req.Action == "finish" {
		score, seconds := 0, session.PlaySeconds
		if won {
			score = state.Difficulty
		}
		if over {
			seconds = session.PlayedSeconds(common.GetTimestamp())
		}
		seconds = min(seconds, steps*puzzle.secondsPerStep)
		result := settleSkillSession(c, puzzle.name, rule, session, skillOutcome{Score: score, Seconds: seconds, State: string(data), Detail: gin.H{"difficulty": state.Difficulty, "moves": steps, "won": won}})
		if result == nil {
			return
		}
		view["status"], view["result"] = model.GameSessionStatusFinished, result
		if key == "bulls" {
			// 结束了就公开答案(主动结束时引擎里还没有结束)。
			if game, err := bulls.New(state.Difficulty, uint64(session.Seed)); err == nil {
				view["secret"] = game.Secret
			}
		}
	} else if err := model.UpdateGameSessionState(session, string(data), ""); err != nil {
		respondGameError(c, key, err)
		return
	}
	view["version"] = session.Version
	common.ApiSuccess(c, view)
}
