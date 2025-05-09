package middleware // 或 commonMiddleware

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Xushengqwer/go-common/constants"    // 导入包含 Context Key 的包
	"github.com/Xushengqwer/go-common/models/enums" // 导入包含枚举和转换函数的包
	"github.com/Xushengqwer/go-common/response"     // 导入标准响应包
	"github.com/gin-gonic/gin"
)

// UserContextMiddleware (重构版)
// 职责: 从请求头读取用户信息，验证其有效性，并将验证后的信息存入标准 Go Context。
// 如果任何必需信息缺失或无效，则中止请求并返回 401 或 400 错误。
func UserContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 读取必需的 Header
		userID := c.GetHeader("X-User-ID")
		roleStr := c.GetHeader("X-User-Role")
		statusStr := c.GetHeader("X-User-Status")
		platformStr := c.GetHeader("X-Platform")

		// 2. 严格校验必需 Header 是否存在
		if userID == "" {
			response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "缺少 X-User-ID 请求头")
			c.Abort()
			return
		}
		if roleStr == "" {
			response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "缺少 X-User-Role 请求头")
			c.Abort()
			return
		}
		if statusStr == "" {
			response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "缺少 X-User-Status 请求头")
			c.Abort()
			return
		}
		if platformStr == "" {
			response.RespondError(c, http.StatusUnauthorized, response.ErrCodeClientUnauthorized, "缺少 X-Platform 请求头")
			c.Abort()
			return
		}

		// 3. 验证并转换 Role (假设 Role 的 String() 返回小写 "admin", "user", "guest")
		//    你需要一个从 string 转换回 UserRole 的方法，这里假设叫 RoleFromString
		var userRole enums.UserRole
		var err error
		// 假设 RoleFromString(roleStr) (enums.UserRole, error) 存在于你的 enums 包
		// 如果不存在，你需要添加它，或者直接比较字符串
		userRole, err = enums.RoleFromString(roleStr) // 示例调用，需要你实现 RoleFromString
		if err != nil {
			response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, fmt.Sprintf("无效的 X-User-Role 值: %s", roleStr))
			c.Abort()
			return
		}

		// 4. 验证并转换 Status (假设 Status 的 String() 返回 "active", "blacklisted")
		//    你需要一个从 string 转换回 UserStatus 的方法，这里假设叫 StatusFromString
		var userStatus enums.UserStatus
		// 假设 StatusFromString(statusStr) (enums.UserStatus, error) 存在于你的 enums 包
		userStatus, err = enums.StatusFromString(statusStr) // 示例调用，需要你实现 StatusFromString
		if err != nil {
			response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, fmt.Sprintf("无效的 X-User-Status 值: %s", statusStr))
			c.Abort()
			return
		}

		// 5. 验证 Platform (使用你已有的 PlatformFromString)
		userPlatform, err := enums.PlatformFromString(platformStr)
		if err != nil {
			response.RespondError(c, http.StatusBadRequest, response.ErrCodeClientInvalidInput, fmt.Sprintf("无效的 X-Platform 值: %s", platformStr))
			c.Abort()
			return
		}

		// 6. 所有信息有效，创建新的 Context 并存入值
		newCtx := c.Request.Context()
		newCtx = context.WithValue(newCtx, constants.UserIDKey, userID)
		newCtx = context.WithValue(newCtx, constants.RoleKey, userRole)         // 存储枚举类型
		newCtx = context.WithValue(newCtx, constants.StatusKey, userStatus)     // 存储枚举类型
		newCtx = context.WithValue(newCtx, constants.PlatformKey, userPlatform) // 存储枚举类型

		// 7. 替换请求中的 Context
		c.Request = c.Request.WithContext(newCtx)

		// 8. (可选) 移除 c.Set 调用
		// c.Set("UserID", userID) // 不再需要

		// 9. 继续处理链
		c.Next()
	}
}
