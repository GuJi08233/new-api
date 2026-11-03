// 导出真实历练开局，交给 simulate.mjs 在浏览器同一份规则里运行固定操作策略。
package main

import (
	"fmt"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/doufa"
	"github.com/QuantumNous/new-api/pkg/dungeon"
	"github.com/QuantumNous/new-api/pkg/wendao"
)

type scenario struct {
	ID       string      `json:"id"`
	Mode     string      `json:"mode"`
	Chapter  int         `json:"chapter"`
	Step     int         `json:"step,omitempty"`
	Training string      `json:"training"`
	Class    string      `json:"class"`
	Setup    doufa.Setup `json:"setup"`
}

// cultivatedHero 的三个样本是明确的修行投入，均使用本流派起手法宝与法术，不依赖宗门特产、丹符、弟子或灵兽。
func cultivatedHero(class, training string, chapter int) *wendao.Hero {
	h, err := wendao.NewHero(class, 0)
	if err != nil {
		panic(err)
	}
	h.Realm = chapter
	if training == "base" {
		return h
	}
	h.Layer = wendao.MaxLayer
	level := chapter + 1
	h.Cave[wendao.RoomSpring] = level
	h.Tempered = wendao.TemperCap(chapter) / 2
	h.Worn = append(h.Worn, dungeon.RelicIronPlate)
	for _, relic := range h.Worn {
		h.Refine[relic] = level
	}
	for i := range h.Cards {
		if i < 6 {
			h.Cards[i].Up = true
		}
	}
	if training == "trained" {
		return h
	}
	h.Cave[wendao.RoomSpring] = wendao.RoomCap(chapter)
	h.Tempered = wendao.TemperCap(chapter)
	if chapter >= 3 {
		h.Golden = wendao.GoldenMax
	}
	for _, relic := range []string{dungeon.RelicJade, dungeon.RelicRune, dungeon.RelicMetronome, dungeon.RelicCrystal} {
		if len(h.Worn) < h.RelicSlots() {
			h.Worn = append(h.Worn, relic)
		}
	}
	for _, relic := range h.Worn {
		h.Refine[relic] = wendao.RoomCap(chapter)
	}
	for i := range h.Cards {
		h.Cards[i].Up = true
	}
	return h
}

func main() {
	var out []scenario
	for _, class := range dungeon.Classes {
		for _, training := range []string{"base", "trained", "graduate"} {
			for _, node := range wendao.JourneyNodes {
				if node.Kind != wendao.NodeBoss || node.Side != "" {
					continue
				}
				h := cultivatedHero(class, training, node.Chapter)
				hp, dmg := wendao.EnemyScale(&node)
				out = append(out, scenario{ID: node.Id, Mode: "chapter", Chapter: node.Chapter + 1, Training: training, Class: class,
					Setup: h.JourneySetup(node.Kind, node.Enemies, hp, dmg, node.Chapter)})
			}
			for _, step := range wendao.Steps() {
				h := cultivatedHero(class, training, wendao.MaxRealm)
				out = append(out, scenario{ID: fmt.Sprintf("step_%02d", step.Step), Mode: "steps", Chapter: wendao.Chapters,
					Step: step.Step, Training: training, Class: class, Setup: h.JourneySetup(step.Kind, step.Enemies, step.HP, step.Dmg, wendao.MaxRealm)})
			}
		}
		// 五种天象在各章的三类关卡：样本取每类第一个节点，养成固定为同境合理培养。
		for chapter := 0; chapter < wendao.Chapters; chapter++ {
			for _, kind := range []string{wendao.NodeCombat, wendao.NodeElite, wendao.NodeBoss} {
				for _, node := range wendao.JourneyNodes {
					if node.Chapter != chapter || node.Kind != kind || node.Side != "" {
						continue
					}
					for _, sign := range wendao.WeeklySigns {
						h := cultivatedHero(class, "trained", chapter)
						hp, dmg := wendao.EnemyScale(&node)
						enemies := append([]string(nil), node.Enemies...)
						if sign.Extra {
							enemies = append(enemies, wendao.Regions[chapter].Foes[0])
						}
						setup := h.JourneySetup(kind, enemies, max(50, hp*sign.HP/100), max(50, dmg*sign.Dmg/100), chapter)
						setup.Mods.Damage += 5 * sign.Strength
						setup.Mods.Vigor += sign.Block
						setup.Mods.Ward += sign.Block / 12
						out = append(out, scenario{ID: fmt.Sprintf("%s_%s_%s", node.Id, kind, sign.Key), Mode: "weekly", Chapter: chapter + 1,
							Training: "trained", Class: class, Setup: setup})
					}
					break
				}
			}
		}
	}
	data, err := common.Marshal(out)
	if err != nil {
		panic(err)
	}
	if _, err := os.Stdout.Write(data); err != nil {
		panic(err)
	}
}
