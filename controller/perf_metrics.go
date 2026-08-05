package controller

import (
	"net/http"
	"strconv"
	"time"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// perfSummaryModelLimit caps how many models a single summary response carries.
// The three performance leaderboards in the UI show far fewer rows than this,
// and every model carries an inline series, so an uncapped response would grow
// with the model catalogue for no visible benefit.
const perfSummaryModelLimit = 100

func GetPerfMetricsSummary(c *gin.Context) {
	// The rankings page drives every tab from one period selector, so the
	// performance summary honours the same calendar windows. "hours" stays
	// supported for the dashboard widgets that ask for a rolling window.
	window, err := service.ResolveRankingPeriod(c.DefaultQuery("period", "week"), time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid period",
		})
		return
	}
	if rawHours := c.Query("hours"); rawHours != "" {
		hours, err := strconv.Atoi(rawHours)
		if err != nil || hours <= 0 || hours > 24*365 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "invalid hours",
			})
			return
		}
		window.End = time.Now().Unix()
		window.Start = window.End - int64(hours)*3600
	}

	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	result, err := perfmetrics.QuerySummaryAll(perfmetrics.SummaryParams{
		StartTs: window.Start,
		EndTs:   window.End,
		Groups:  activeGroups,
		Limit:   perfSummaryModelLimit,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
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

func GetPerfMetrics(c *gin.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model: modelName,
		Group: c.Query("group"),
		Hours: hours,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	result.Groups = filterActiveGroups(result.Groups)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

func filterActiveGroups(groups []perfmetrics.GroupResult) []perfmetrics.GroupResult {
	activeRatios := ratio_setting.GetGroupRatioCopy()
	return lo.Filter(groups, func(g perfmetrics.GroupResult, _ int) bool {
		_, ok := activeRatios[g.Group]
		return ok || g.Group == "auto"
	})
}
