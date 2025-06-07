package router

import (
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"time"

	"github.com/Xushengqwer/go-common/core"
	commonMiddleware "github.com/Xushengqwer/go-common/middleware"
	// ... 其他导入 ...
	appConfig "github.com/Xushengqwer/post_service/config"
	"github.com/Xushengqwer/post_service/constant" // 需要导入常量包获取 ServiceName
	"github.com/Xushengqwer/post_service/controller"
	"github.com/gin-gonic/gin"
	// 导入 OTel Gin 中间件
	otelgin "go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"net/http"
)

// SetupRouter 仅负责配置 Gin 引擎、中间件和路由注册。
func SetupRouter(
	logger *core.ZapLogger,
	cfg *appConfig.PostConfig,
	postController *controller.PostController,
	hotPostController *controller.HotPostController,
	postAdminController *controller.PostAdminController,
) *gin.Engine {
	logger.Info("开始设置 Gin 路由...")

	// 使用 gin.New() 而不是 gin.Default()，因为我们要自定义 Recovery 和 Logger
	router := gin.New()

	// 1. OTel Middleware (最先，处理追踪上下文和 Span)
	router.Use(otelgin.Middleware(constant.ServiceName))

	// 2. Panic Recovery (捕获后续中间件和 handler 的 panic)
	router.Use(commonMiddleware.ErrorHandlingMiddleware(logger))

	// 3. Request Logger (记录访问日志，需要 TraceID)
	// 注意：你的 RequestLoggerMiddleware 需要 *zap.Logger，而你注入的是 *core.ZapLogger
	// 你需要将 core.ZapLogger 适配一下，或者修改中间件接收 core.ZapLogger
	// 假设你的 core.ZapLogger 有一个方法 .Logger() 返回底层的 *zap.Logger
	if baseLogger := logger.Logger(); baseLogger != nil {
		router.Use(commonMiddleware.RequestLoggerMiddleware(baseLogger))
	} else {
		logger.Warn("无法获取底层的 *zap.Logger，跳过 RequestLoggerMiddleware 注册")
	}

	// 4. Request Timeout (超时控制)
	// 假设配置中的 RequestTimeout 是秒数
	requestTimeout := time.Duration(cfg.ServerConfig.RequestTimeout) * time.Second
	router.Use(commonMiddleware.RequestTimeoutMiddleware(logger, requestTimeout))

	// 5. User Context (提取用户信息)
	router.Use(commonMiddleware.UserContextMiddleware())

	// 6. (可选) RequestIDMiddleware - 如果决定需要它，放在 OTel 之后，Logger 之前或之后都可以
	// router.Use(middleware.RequestIDMiddleware()) // 根据你的决定选择是否添加

	logger.Debug("已注册全局中间件")

	// --- 创建 API 版本分组 ---
	v1 := router.Group("/api/v1/post")
	logger.Debug("已创建 API/v1/post 分组")

	// --- 注册控制器路由 ---
	postController.RegisterRoutes(v1)
	hotPostController.RegisterRoutes(v1)
	postAdminController.RegisterRoutes(v1)
	logger.Info("所有控制器路由已注册到 /api/v1/post 分组")

	// --- 新增：注册 Swagger UI 路由 ---
	// 访问 /swagger/index.html 即可看到 Swagger UI 界面
	// ginSwagger.WrapHandler 会处理 swagger.json 的加载和 UI 渲染
	swaggerURL := ginSwagger.URL("/swagger/doc.json") // 指定 swagger.json 的访问路径
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, swaggerURL))
	logger.Info("Swagger UI endpoint registered at /swagger/*any")

	// --- 健康检查等路由 ---
	router.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	logger.Info("Gin 路由器设置完成")
	return router
}
