package middleware

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// RequireSecurityProof 证明只授权当前用户、会话、操作和参数，数据库原子核销。
func RequireSecurityProof(c *gin.Context, scope, context string) bool {
	identity, err := CookieSecurityIdentity(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": err.Error()})
		return false
	}
	if _, err := model.ReadSecurityIdentity(identity); err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"success": false, "message": err.Error()})
		return false
	}
	digest := model.SecurityTokenHash(context)
	token := c.GetHeader("X-Security-Proof")
	if token != "" {
		if _, err := model.ConsumeSecurityFlow(token, "proof", identity, scope, digest); err == nil {
			return true
		}
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "VERIFICATION_REQUIRED", "message": "需要安全验证", "scope": scope, "context_hash": digest})
	return false
}

func SecureVerificationRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil || id <= 0 {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		if !RequireSecurityProof(c, "channel.key.read", strconv.Itoa(id)) {
			return
		}
		c.Next()
	}
}

func ClearSecureVerification(c *gin.Context) {
	session := sessions.Default(c)
	session.Delete("secure_verified_at")
	session.Delete("secure_verified_method")
	session.Delete("secure_passkey_ready_at")
	_ = session.Save()
}
