package controller

import (
	"fmt"
	"net/http"
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

func GetUserRankings(c *gin.Context) {
	period := c.DefaultQuery("period", "week")
	duration, err := userRankingDuration(period)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period",
		})
		return
	}

	now := time.Now()
	endTime := now.Unix()
	startTime := now.Add(-duration).Unix()

	type queryResult[T any] struct {
		data T
		err  error
	}

	var (
		reqCh  = make(chan queryResult[[]model.UserRequestRanking], 1)
		quotaCh = make(chan queryResult[[]model.UserQuotaRanking], 1)
		sumCh  = make(chan queryResult[*model.UserRankingSummary], 1)
	)

	go func() {
		rows, err := model.GetUserRequestRankings(startTime, endTime, 50)
		reqCh <- queryResult[[]model.UserRequestRanking]{rows, err}
	}()
	go func() {
		rows, err := model.GetUserQuotaRankings(startTime, endTime, 50)
		quotaCh <- queryResult[[]model.UserQuotaRanking]{rows, err}
	}()
	go func() {
		summary, err := model.GetUserRankingSummary(startTime, endTime)
		sumCh <- queryResult[*model.UserRankingSummary]{summary, err}
	}()

	reqRes := <-reqCh
	quotaRes := <-quotaCh
	sumRes := <-sumCh

	if reqRes.err != nil || quotaRes.err != nil || sumRes.err != nil {
		if reqRes.err != nil {
			common.SysError("user request rankings error: " + reqRes.err.Error())
		}
		if quotaRes.err != nil {
			common.SysError("user quota rankings error: " + quotaRes.err.Error())
		}
		if sumRes.err != nil {
			common.SysError("user ranking summary error: " + sumRes.err.Error())
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to load rankings",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"request_rankings": reqRes.data,
			"quota_rankings":   quotaRes.data,
			"summary":          sumRes.data,
		},
	})
}

func userRankingDuration(period string) (time.Duration, error) {
	switch period {
	case "today":
		return 24 * time.Hour, nil
	case "", "week":
		return 7 * 24 * time.Hour, nil
	case "month":
		return 30 * 24 * time.Hour, nil
	case "year":
		return 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid period: %s", period)
	}
}
