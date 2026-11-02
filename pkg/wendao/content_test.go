package wendao

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// contentManifestPath 是规则导出的内容清单。前端的 web/classic/src/pages/Games/wendao/content.test.js 读它，核对每个
// 节点、敌人、机缘、法宝、丹符、洞府的屋子、丹方、灵兽……都有文案与画像。规则里的内容变了，用
// `UPDATE_CONTENT=1 go test ./pkg/wendao -run TestContentManifestIsCurrent` 重新生成，再在 web/classic 里跑
// `bun test src/pages/Games/wendao`。
const contentManifestPath = "testdata/content.json"

// manifestNode 是清单里的一个历练节点：只有前端要对上的部分，数值不在里面，调数值不用重新生成清单。
type manifestNode struct {
	Id       string   `json:"id"`
	Chapter  int      `json:"chapter"`
	Kind     string   `json:"kind"`
	Requires []string `json:"requires"`
	Enemies  []string `json:"enemies,omitempty"`
	Relic    string   `json:"relic,omitempty"`
	Potions  []string `json:"potions,omitempty"`
	Key      string   `json:"key,omitempty"`
}

// 内容清单跟规则一致：前端的内容检查读的是这份清单，规则改了清单没跟上，前端的检查就查不到新加的内容。
func TestContentManifestIsCurrent(t *testing.T) {
	var fields []string
	add := func(key string, value any) {
		data, err := common.Marshal(value)
		require.NoError(t, err)
		fields = append(fields, fmt.Sprintf("  %q: %s", key, data))
	}
	add("chapters", Chapters)
	add("break_keys", BreakKeys)

	nodes := make([]string, 0, len(JourneyNodes))
	for _, n := range JourneyNodes {
		data, err := common.Marshal(manifestNode{Id: n.Id, Chapter: n.Chapter, Kind: n.Kind, Requires: n.Requires,
			Enemies: n.Enemies, Relic: n.Relic, Potions: n.Potions, Key: n.Key})
		require.NoError(t, err)
		nodes = append(nodes, "    "+string(data))
	}
	fields = append(fields, "  \"nodes\": [\n"+strings.Join(nodes, ",\n")+"\n  ]")

	// 问心阶的心魔化作的敌人，每种只记一次。
	stepEnemies := []string{}
	seen := map[string]bool{}
	for _, st := range Steps() {
		for _, e := range st.Enemies {
			if !seen[e] {
				seen[e] = true
				stepEnemies = append(stepEnemies, e)
			}
		}
	}
	add("step_enemies", stepEnemies)

	var signs, elixirs, baits, crops, potions []string
	for _, s := range WeeklySigns {
		signs = append(signs, s.Key)
	}
	for _, r := range ElixirRecipes {
		elixirs = append(elixirs, r.Id)
	}
	for _, b := range Baits {
		baits = append(baits, b.Key)
	}
	for _, c := range Crops {
		crops = append(crops, c.Key)
	}
	for _, r := range PotionRecipes {
		potions = append(potions, r.Potion)
	}
	add("signs", signs)
	add("rooms", RoomOrder)
	add("refine_kinds", []string{RefineVital, RefineGuard, RefineEdge})
	add("elixirs", elixirs)
	add("beasts", BeastKinds)
	add("baits", baits)
	add("pills", PillKeys)
	add("rares", RareKeys)
	add("crops", crops)
	add("potions", potions)
	want := "{\n" + strings.Join(fields, ",\n") + "\n}\n"

	if os.Getenv("UPDATE_CONTENT") != "" {
		require.NoError(t, os.WriteFile(contentManifestPath, []byte(want), 0o644))
	}
	got, err := os.ReadFile(contentManifestPath)
	require.NoError(t, err)
	assert.Equal(t, want, strings.ReplaceAll(string(got), "\r\n", "\n"),
		"%s is stale: run UPDATE_CONTENT=1 go test ./pkg/wendao -run TestContentManifestIsCurrent, "+
			"then bun test src/pages/Games/wendao in web/classic", contentManifestPath)
}
