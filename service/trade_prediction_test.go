package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradePredictionMatchesDepthFeesAndLimit(t *testing.T) {
	book := &tradePredictionBook{Asks: []tradePredictionLevel{{"0.6", "10"}, {"0.5", "10"}}, Bids: []tradePredictionLevel{{"0.4", "5"}, {"0.6", "5"}}, expiresAtMs: 123}
	for _, test := range []struct {
		name                  string
		buy                   bool
		amount, shares, limit string
		qty                   int64
		amountQuota, fee      int
		err                   error
	}{
		{"buy depth", true, "20", "0", "0.7", 2_000_000_000, 11_000_000, 343_000, nil},
		{"buy limit", true, "20", "0", "0.55", 1_000_000_000, 5_000_000, 175_000, nil},
		{"buy includes fee", true, "5.175", "0", "0.55", 1_000_000_000, 5_000_000, 175_000, nil},
		{"sell partial liquidity", false, "0", "20", "0.3", 1_000_000_000, 5_000_000, 168_000, nil},
		{"sell limit", false, "0", "20", "0.5", 500_000_000, 3_000_000, 84_000, nil},
		{"reject changed odds", true, "20", "0", "0.4", 0, 0, 0, ErrTradeNoLiquidity},
		{"reject zero limit", true, "20", "0", "0", 0, 0, 0, ErrTradeOrderInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			fill, err := matchPredictionBook(book, test.buy, decimal.RequireFromString(test.amount), decimal.RequireFromString(test.shares), decimal.RequireFromString(test.limit), 1_000_000)
			if test.err != nil {
				require.ErrorIs(t, err, test.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.qty, fill.Qty)
			assert.Equal(t, test.amountQuota, fill.Amount)
			assert.Equal(t, test.fee, fill.Fee)
		})
	}
	book.Asks = []tradePredictionLevel{{"1e100000000", "10"}}
	_, err := matchPredictionBook(book, true, decimal.NewFromInt(10), decimal.Zero, decimal.RequireFromString("0.9"), 1_000_000)
	require.ErrorIs(t, err, model.ErrTradePredictionQuote)
}

// 成交金额与手续费各自向上取整到额度单位时可能比预算多出一两个单位；买入要照样成交，合起来不超过预算。
func TestTradePredictionBuyStaysWithinBudgetAfterRounding(t *testing.T) {
	book := &tradePredictionBook{Asks: []tradePredictionLevel{{"0.001", "100000"}}}
	fill, err := matchPredictionBook(book, true, decimal.NewFromInt(1), decimal.Zero, decimal.RequireFromString("0.001"), 500_000)
	require.NoError(t, err)
	assert.Equal(t, int64(93_464_050_000), fill.Qty)
	assert.Equal(t, 500_000, fill.Amount+fill.Fee)
	assert.Equal(t, 32_680, fill.Fee)
}

func predictionTestClient(t *testing.T, handler http.HandlerFunc) *tradePredictionClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := newTradePredictionClient()
	client.gammaURL, client.clobURL, client.cryptoURL = server.URL, server.URL, server.URL
	client.transport = func(string) (*http.Client, error) { return server.Client(), nil }
	return client
}

func TestTradePredictionBookRejectsStaleAndWrongMarket(t *testing.T) {
	previous := operation_setting.GetTradeSetting()
	setting := operation_setting.DefaultTradeSetting()
	setting.Enabled = true
	operation_setting.SetTradeSettingForTest(setting)
	t.Cleanup(func() { operation_setting.SetTradeSettingForTest(previous) })
	for _, test := range []struct {
		name, token, condition string
		age                    time.Duration
		valid                  bool
	}{
		{"fresh", "111", "condition", 0, true}, {"stale", "111", "condition", 10 * time.Second, false}, {"wrong token", "222", "condition", 0, false}, {"wrong round", "111", "other", 0, false}, {"future clock", "111", "condition", -10 * time.Second, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := predictionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "111", r.URL.Query().Get("token_id"))
				fmt.Fprintf(w, `{"market":%q,"asset_id":%q,"timestamp":%q,"asks":[{"price":"0.5","size":"10"}],"bids":[]}`, test.condition, test.token, strconv.FormatInt(time.Now().Add(-test.age).UnixMilli(), 10))
			})
			_, err := client.book(context.Background(), &model.TradePredictionRound{ConditionId: "condition", UpToken: "111", DownToken: "222"}, model.TradePredictionUp)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, model.ErrTradePredictionQuote)
			}
		})
	}
}

func TestTradePredictionOnlyOfficialUniqueWinnerSettles(t *testing.T) {
	round := model.TradePredictionRound{Slug: "btc-updown-5m-test", ConditionId: "condition", UpToken: "111", DownToken: "222"}
	open := `{"condition_id":"condition","closed":false,"tokens":[{"token_id":"111","outcome":"Up","price":0.6},{"token_id":"222","outcome":"Down","price":0.4}]}`
	gamma := func(closed bool, resolution, prices, tokens string) string {
		return fmt.Sprintf(`[{"slug":"btc-updown-5m-test","conditionId":"condition","closed":%t,"umaResolutionStatus":%q,"outcomes":"[\"Up\", \"Down\"]","outcomePrices":%q,"clobTokenIds":%q}]`, closed, resolution, prices, tokens)
	}
	for _, test := range []struct{ name, clob, gamma, winner string }{
		{"official up", `{"condition_id":"condition","closed":true,"tokens":[{"token_id":"111","outcome":"Up","winner":true},{"token_id":"222","outcome":"Down","winner":false}]}`, "[]", "UP"},
		{"probability is not result", `{"condition_id":"condition","closed":true,"tokens":[{"token_id":"111","outcome":"Up","price":1},{"token_id":"222","outcome":"Down","price":0}]}`, "[]", ""},
		{"not closed", `{"condition_id":"condition","closed":false,"tokens":[{"token_id":"111","outcome":"Up","winner":true},{"token_id":"222","outcome":"Down"}]}`, "[]", ""},
		{"two winners", `{"condition_id":"condition","closed":true,"tokens":[{"token_id":"111","outcome":"Up","winner":true},{"token_id":"222","outcome":"Down","winner":true}]}`, "[]", ""},
		{"wrong token", `{"condition_id":"condition","closed":true,"tokens":[{"token_id":"333","outcome":"Up","winner":true},{"token_id":"222","outcome":"Down"}]}`, "[]", ""},
		{"fifty-fifty", `{"condition_id":"condition","closed":true,"is_50_50_outcome":true,"tokens":[{"token_id":"111","outcome":"Up","price":0.5,"winner":false},{"token_id":"222","outcome":"Down","price":0.5,"winner":false}]}`, "[]", model.TradePredictionSplit},
		{"fifty-fifty flag without half prices", `{"condition_id":"condition","closed":true,"is_50_50_outcome":true,"tokens":[{"token_id":"111","outcome":"Up","price":1},{"token_id":"222","outcome":"Down","price":0}]}`, "[]", ""},
		{"fifty-fifty still open", `{"condition_id":"condition","closed":false,"is_50_50_outcome":true,"tokens":[{"token_id":"111","outcome":"Up","price":0.5},{"token_id":"222","outcome":"Down","price":0.5}]}`, "[]", ""},
		// CLOB 结算后要过一阵才标赢家，Gamma 上已经 resolved 的结算价先用。
		{"gamma resolved before clob", open, gamma(true, "resolved", `["0", "1"]`, `["111", "222"]`), "DOWN"},
		{"gamma fifty-fifty", open, gamma(true, "resolved", `["0.5", "0.5"]`, `["111", "222"]`), model.TradePredictionSplit},
		{"gamma probabilities are not result", open, gamma(true, "resolved", `["0.995", "0.005"]`, `["111", "222"]`), ""},
		{"gamma proposal is not result", open, gamma(true, "proposed", `["1", "0"]`, `["111", "222"]`), ""},
		{"gamma open market", open, gamma(false, "resolved", `["1", "0"]`, `["111", "222"]`), ""},
		{"gamma wrong tokens", open, gamma(true, "resolved", `["1", "0"]`, `["222", "111"]`), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := predictionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				payload := test.clob
				if r.URL.Path == "/markets" {
					assert.Equal(t, "true", r.URL.Query().Get("closed"))
					assert.Equal(t, round.Slug, r.URL.Query().Get("slug"))
					payload = test.gamma
				} else {
					assert.Equal(t, "/markets/condition", r.URL.Path)
				}
				_, err := w.Write([]byte(payload))
				assert.NoError(t, err)
			})
			winner, err := client.officialOutcome(context.Background(), round)
			if test.winner == "" {
				require.ErrorIs(t, err, model.ErrTradePredictionOutcome)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.winner, winner)
			}
		})
	}
}

func TestTradePredictionDiscoveryMapsOutcomeOrderAndEndTime(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.TradePredictionRound{}))
	window := common.GetTimestamp() / 300 * 300
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("window_start = ?", window).Delete(&model.TradePredictionRound{}).Error)
	})
	client := predictionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "btc-updown-5m-"+strconv.FormatInt(window, 10), r.URL.Query().Get("slug"))
		fmt.Fprintf(w, `[{"slug":%q,"conditionId":"condition","outcomes":"[\"Down\",\"Up\"]","clobTokenIds":"[\"222\",\"111\"]","endDate":%q,"acceptingOrders":true}]`, r.URL.Query().Get("slug"), time.Unix(window+300, 0).UTC().Format(time.RFC3339))
	})
	round, err := client.discover(context.Background(), window)
	require.NoError(t, err)
	assert.Equal(t, "111", round.UpToken)
	assert.Equal(t, "222", round.DownToken)
	assert.Equal(t, window+300, round.EndTime)
}

func TestTradePredictionRecoveryContinuesAfterRestartAndWhenDisabled(t *testing.T) {
	require.NoError(t, model.DB.AutoMigrate(&model.TradePredictionRound{}, &model.TradePredictionPosition{}))
	window := common.GetTimestamp()/300*300 - 300
	userId := 6271
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("window_start = ?", window).Delete(&model.TradePredictionPosition{}).Error)
		require.NoError(t, model.DB.Where("window_start = ?", window).Delete(&model.TradePredictionRound{}).Error)
		require.NoError(t, model.DB.Where("user_id = ?", userId).Delete(&model.TradeLedger{}).Error)
		require.NoError(t, model.DB.Where("user_id = ?", userId).Delete(&model.TradeAccount{}).Error)
	})
	round := model.TradePredictionRound{WindowStart: window, EndTime: window + 300, ConditionId: "condition", UpToken: "111", DownToken: "222", Slug: "test"}
	require.NoError(t, model.SaveTradePredictionRound(round))
	position := model.TradePredictionPosition{UserId: userId, WindowStart: window, Side: "DOWN", Status: model.TradePredictionActive, Qty: 2 * 100_000_000, InitialQty: 2 * 100_000_000, Cost: tradeTestUsd("0.8"), TotalCost: tradeTestUsd("0.8")}
	require.NoError(t, model.DB.Create(&position).Error)
	previous := operation_setting.GetTradeSetting()
	operation_setting.SetTradeSettingForTest(operation_setting.DefaultTradeSetting())
	t.Cleanup(func() { operation_setting.SetTradeSettingForTest(previous) })
	resolved := false
	client := predictionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/markets" {
			fmt.Fprint(w, "[]")
			return
		}
		fmt.Fprintf(w, `{"condition_id":"condition","closed":%t,"tokens":[{"token_id":"111","outcome":"Up","winner":false},{"token_id":"222","outcome":"Down","winner":%t}]}`, resolved, resolved)
	})
	client.settlePending(context.Background())
	stored, err := model.GetTradePredictionPosition(userId, position.Id)
	require.NoError(t, err)
	assert.Equal(t, model.TradePredictionActive, stored.Status)
	resolved = true
	client.settlePending(context.Background())
	client.settlePending(context.Background())
	stored, err = model.GetTradePredictionPosition(userId, position.Id)
	require.NoError(t, err)
	assert.Equal(t, model.TradePredictionWon, stored.Status)
	assert.Equal(t, tradeTestUsd("2"), stored.Payout)
	account, err := model.GetTradeAccount(userId)
	require.NoError(t, err)
	assert.Equal(t, tradeTestUsd("2"), account.Cash)
}
