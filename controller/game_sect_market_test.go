package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/wendao"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sectOf(t *testing.T, player *gamePlayer) *wendao.Sect {
	t.Helper()
	data, err := model.GetGameSave(player.user.Id, wendao.SaveKey)
	require.NoError(t, err)
	s, err := wendao.ParseSect(data)
	require.NoError(t, err)
	return s
}

// 万宝阁一整轮：主殿不到 2 级不能挂单；挂单把货物押走，别人买下时买家付灵石拿货、卖家收到扣掉抽成的灵石，这一单不能再买；
// 指名的单只有那位道友能买、可以 0 灵石赠送；撤单把货物放回；过期的单买不了；同时挂的单有上限。
func TestSectMarketThroughTheApi(t *testing.T) {
	require.NoError(t, i18n.Init())
	env := newGameTestEnv(t, sectSettings())
	seller := env.player(0, gameTestIpA)
	buyer := env.player(0, gameTestIpB)
	other := env.player(0, gameTestIpB)
	path := "/api/game/wendao/sect/market"
	for _, p := range []*gamePlayer{seller, buyer, other} {
		sectView(t, p, map[string]any{"action": "sync"})
	}

	message := seller.fails(http.MethodPost, path+"/list", map[string]any{"goods": "pill_heal", "count": 1, "price": 50})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameMarketHall), message)
	for _, p := range []*gamePlayer{seller, buyer, other} {
		editSect(t, p, func(s *wendao.Sect) {
			s.Buildings[wendao.BuildingHall] = 2
			s.Res.Stone = 1000
			s.Pills[wendao.PillHeal] = 5
			s.Rare[wendao.RareLotus] = 1
		})
	}
	seller.fails(http.MethodPost, path+"/list", map[string]any{"goods": "pill_heal", "count": 6, "price": 50})
	seller.fails(http.MethodPost, path+"/list", map[string]any{"goods": "pill_heal", "count": 1, "price": 0})
	listed := seller.ok(http.MethodPost, path+"/list", map[string]any{"goods": "pill_heal", "count": 3, "price": 300})
	mine := listed["mine"].([]any)
	require.Len(t, mine, 1)
	id := mine[0].(map[string]any)["id"]
	assert.Equal(t, float64(2), listed["sect"].(map[string]any)["pills"].(map[string]any)["heal"])

	browse := buyer.ok(http.MethodGet, path, nil)
	require.Len(t, browse["listings"], 1)
	message = seller.fails(http.MethodPost, path+"/buy", map[string]any{"id": id})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameMarketOwn), message)
	bought := buyer.ok(http.MethodPost, path+"/buy", map[string]any{"id": id})
	assert.Empty(t, bought["listings"])
	require.Len(t, bought["trades"], 1)
	assert.Equal(t, float64(15), bought["trades"].([]any)[0].(map[string]any)["tax"])
	assert.Equal(t, 8, sectOf(t, buyer).Pills[wendao.PillHeal])
	assert.GreaterOrEqual(t, sectOf(t, buyer).Res.Stone, 700)
	assert.GreaterOrEqual(t, sectOf(t, seller).Res.Stone, 1000+285)
	message = other.fails(http.MethodPost, path+"/buy", map[string]any{"id": id})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameMarketGone), message)

	gift := seller.ok(http.MethodPost, path+"/list", map[string]any{"goods": "rare_lotus", "count": 1, "price": 0, "target": buyer.user.Id})
	giftId := gift["mine"].([]any)[0].(map[string]any)["id"]
	assert.Empty(t, other.ok(http.MethodGet, path, nil)["offers"])
	assert.Empty(t, buyer.ok(http.MethodGet, path, nil)["listings"], "directed listings stay out of the public list")
	require.Len(t, buyer.ok(http.MethodGet, path, nil)["offers"], 1)
	other.fails(http.MethodPost, path+"/buy", map[string]any{"id": giftId})
	buyer.ok(http.MethodPost, path+"/buy", map[string]any{"id": giftId})
	assert.Equal(t, 2, sectOf(t, buyer).Rare[wendao.RareLotus])
	assert.Equal(t, 0, sectOf(t, seller).Rare[wendao.RareLotus])

	kept := seller.ok(http.MethodPost, path+"/list", map[string]any{"goods": "pill_heal", "count": 2, "price": 100})
	keptId := kept["mine"].([]any)[0].(map[string]any)["id"]
	other.fails(http.MethodPost, path+"/cancel", map[string]any{"id": keptId})
	require.NoError(t, env.db.Model(&model.WendaoListing{}).Where("id = ?", int(keptId.(float64))).
		Update("expires_at", common.GetTimestamp()-1).Error)
	assert.Empty(t, other.ok(http.MethodGet, path, nil)["listings"], "expired listings are hidden")
	message = other.fails(http.MethodPost, path+"/buy", map[string]any{"id": keptId})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameMarketGone), message)
	seller.ok(http.MethodPost, path+"/cancel", map[string]any{"id": keptId})
	assert.Equal(t, 2, sectOf(t, seller).Pills[wendao.PillHeal])

	editSect(t, other, func(s *wendao.Sect) { s.Res.Herb = 10000 })
	for range wendao.MarketMaxListings {
		other.ok(http.MethodPost, path+"/list", map[string]any{"goods": "herb", "count": 10, "price": 5})
	}
	message = other.fails(http.MethodPost, path+"/list", map[string]any{"goods": "herb", "count": 10, "price": 5})
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgGameMarketTooMany), message)
}
