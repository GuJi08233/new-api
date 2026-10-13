package controller

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetRiskDisabledUsers 分页列出当前被禁用的账号及最近一次禁用事件,附带管理权限。
// query: keyword, kind(temporary|permanent), p, page_size
func GetRiskDisabledUsers(c *gin.Context) {
	pageInfo, pageErr := riskPageQuery(c)
	if pageErr != nil {
		common.ApiError(c, pageErr)
		return
	}
	kind := c.Query("kind")
	if kind != "" && kind != model.RiskDisabledKindTemporary && kind != model.RiskDisabledKindPermanent {
		common.ApiErrorMsg(c, "禁用期限筛选无效")
		return
	}
	users, total, err := model.GetRiskDisabledUsers(model.RiskDisabledUserQuery{
		Keyword: c.Query("keyword"), Kind: kind, Offset: pageInfo.GetStartIdx(), Limit: pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.Id)
	}
	states, err := riskUserStates(ids, c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(users)
	common.ApiSuccess(c, gin.H{"page": pageInfo, "user_states": states})
}

// GetRiskAssignees 返回可分配风控事件的已启用管理员。
func GetRiskAssignees(c *gin.Context) {
	assignees, err := model.GetRiskAssignees()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, assignees)
}

// riskUserLookupLimit 是一次按 ID 查询账号状态的上限。
const riskUserLookupLimit = 200

// GetRiskUsers 按 ID 批量读取账号当前状态与管理权限,用于豁免名单与事件列表显示用户名。
// query: ids=1,2,3
func GetRiskUsers(c *gin.Context) {
	var ids []int
	seen := map[int]bool{}
	for _, part := range strings.Split(c.Query("ids"), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			common.ApiErrorMsg(c, "无效的用户 ID")
			return
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > riskUserLookupLimit {
		common.ApiErrorMsg(c, "一次最多查询 200 个账号")
		return
	}
	states, err := riskUserStates(ids, c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, states)
}

type riskEventReviewBatchRequest struct {
	Items      []model.RiskEventReviewTarget `json:"items"`
	Status     string                        `json:"status"`
	Note       *string                       `json:"note"`
	AssigneeId *int                          `json:"assignee_id"`
}

// ReviewRiskEventsBatch 批量处理事件:逐条检查版本,返回每条的结果。
// status 为空、note 或 assignee_id 省略时保留每条事件原有的值(例如只"分配给我"),但至少要改一项。
func ReviewRiskEventsBatch(c *gin.Context) {
	var request riskEventReviewBatchRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request"})
		return
	}
	if request.Note != nil {
		note := strings.TrimSpace(*request.Note)
		if utf8.RuneCountInString(note) > 2000 {
			common.ApiErrorMsg(c, "处理备注最多 2000 字")
			return
		}
		request.Note = &note
	}
	results, err := model.ReviewRiskEventsBatch(request.Items, request.Status, request.Note, request.AssigneeId, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	updated := make([]int, 0, len(results))
	for _, result := range results {
		if result.Success {
			updated = append(updated, result.Id)
		}
	}
	if len(updated) > 0 {
		params := map[string]interface{}{"event_ids": updated}
		if request.Status != "" {
			params["status"] = request.Status
		}
		if request.AssigneeId != nil {
			params["assignee_id"] = *request.AssigneeId
		}
		recordManageAudit(c, "risk.event.review_batch", params)
	}
	common.ApiSuccess(c, gin.H{"results": results})
}
