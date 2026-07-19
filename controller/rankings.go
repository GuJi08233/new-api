package controller

import (
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetRankings(c *gin.Context) {
	result, err := service.GetRankingsSnapshot(c.DefaultQuery("period", "week"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
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
			"message": err.Error(),
		})
		return
	}

	now := time.Now()
	endTime := now.Unix()
	startTime := now.Add(-duration).Unix()

	requestRankings, err := model.GetUserRequestRankings(startTime, endTime, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	quotaRankings, err := model.GetUserQuotaRankings(startTime, endTime, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	summary, err := model.GetUserRankingSummary(startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"request_rankings": requestRankings,
			"quota_rankings":   quotaRankings,
			"summary":          summary,
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
