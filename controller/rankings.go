package controller

import (
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetRankings(c *gin.Context) {
	result, err := service.GetRankingsSnapshot(c.DefaultQuery("period", "week"))
	if err != nil {
		common.SysError("rankings snapshot error: " + err.Error())
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// userRankingLimit caps each user leaderboard. The page renders one scrollable
// column per board, so a deeper list would only grow the payload.
const userRankingLimit = 50

// userRankingBoards are the three user leaderboards the rankings page shows:
// how many tokens were consumed, how many requests were made, and how much
// quota was spent. Each one is the same aggregation ordered by a different
// metric, so they only differ by the response key.
var userRankingBoards = []struct {
	key    string
	metric model.UserRankingMetric
}{
	{"token_rankings", model.UserRankingByTokens},
	{"request_rankings", model.UserRankingByRequests},
	{"quota_rankings", model.UserRankingByQuota},
}

func GetUserRankings(c *gin.Context) {
	window, err := service.ResolveRankingPeriod(c.DefaultQuery("period", "week"), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period",
		})
		return
	}
	startTime, endTime := window.Start, window.End

	// Each board carries its own ORDER BY and LIMIT, so they stay separate
	// queries and run in parallel rather than serialising three aggregations.
	// Every goroutine owns one slice index, so no extra synchronisation is needed.
	rankings := make([][]model.UserRanking, len(userRankingBoards))
	errs := make([]error, len(userRankingBoards))
	var (
		summary    *model.UserRankingSummary
		summaryErr error
		wg         sync.WaitGroup
	)

	wg.Add(len(userRankingBoards) + 1)
	for i, board := range userRankingBoards {
		go func() {
			defer wg.Done()
			rankings[i], errs[i] = model.GetUserRankings(startTime, endTime, board.metric, userRankingLimit)
		}()
	}
	go func() {
		defer wg.Done()
		summary, summaryErr = model.GetUserRankingSummary(startTime, endTime)
	}()
	wg.Wait()

	failed := summaryErr != nil
	if summaryErr != nil {
		common.SysError("user ranking summary error: " + summaryErr.Error())
	}
	for i, err := range errs {
		if err != nil {
			common.SysError("user " + userRankingBoards[i].key + " error: " + err.Error())
			failed = true
		}
	}
	if failed {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to load rankings",
		})
		return
	}

	data := gin.H{
		"start_time": startTime,
		"end_time":   endTime,
		"summary":    summary,
	}
	for i, board := range userRankingBoards {
		data[board.key] = rankings[i]
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}
