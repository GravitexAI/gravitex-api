package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type CreateOperLogRequest struct {
	OperType string `json:"oper_type"`
	Content  string `json:"content"`
	Remark   string `json:"remark"`
}

// CreateOperLog 创建一条操作日志。
// POST /api/oper-log/
func CreateOperLog(c *gin.Context) {
	var req CreateOperLogRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid parameters"})
		return
	}
	if req.OperType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "oper_type is required"})
		return
	}

	operator := c.GetString("username")

	log := &model.OperLog{
		OperType: req.OperType,
		Content:  req.Content,
		Remark:   req.Remark,
		Operator: operator,
	}
	if err := model.CreateOperLog(log); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": log})
}

// ListOperLogs 分页查询操作日志。
// GET /api/oper-log/?oper_type=&page=1&page_size=20
func ListOperLogs(c *gin.Context) {
	operType := c.Query("oper_type")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	logs, total, err := model.GetOperLogsPaged(operType, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    logs,
		"total":   total,
		"page":    page,
		"size":    pageSize,
	})
}
