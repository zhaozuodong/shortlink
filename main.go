// 短链接服务（单文件示例）
// 功能：
//  - POST /api/shorten  创建短链接（支持自定义短链接）
//  - GET /:code         重定向到原始长链接，并统计点击量
//  - GET /api/info/:code 查询短链接信息（原始链接、点击量、创建时间）
//  - DELETE /api/:code   删除短链接
// 技术栈：Go + Gin + GORM + SQLite（单文件，方便演示和快速启动）
// 运行：
//  1. 安装依赖：
//     go mod init shortlink && go mod tidy
//  2. 启动：
//     go run main.go
//  3. 示例请求：
//     curl -X POST -H "Content-Type: application/json" -d '{"url":"https://example.com/very/long/link"}' http://localhost:8080/api/shorten
//  4. 或者自定义短码：
//     curl -X POST -H "Content-Type: application/json" -d '{"url":"https://example.com","custom":"go123"}' http://localhost:8080/api/shorten

package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// 配置
const (
	DefaultPort     = "8080"
	DefaultCodeLen  = 6
	DomainEnv       = "SHORT_DOMAIN" // 可通过环境变量指定域名（如 https://s.example.com）
	DefaultDomain   = "http://localhost:8080"
	MaxCustomLength = 64
)

// Model
type ShortLink struct {
	ID        uint         `gorm:"primaryKey" json:"-"`
	Code      string       `gorm:"uniqueIndex;size:128" json:"code"`
	TargetURL string       `gorm:"size:2048" json:"url"`
	Clicks    int64        `gorm:"default:0" json:"clicks"`
	CreatedAt time.Time    `json:"created_at"`
	ExpiredAt sql.NullTime `json:"expired_at,omitempty"`
}

// 请求/响应结构
type CreateReq struct {
	URL    string `json:"url"`
	Custom string `json:"custom,omitempty"`      // 可选自定义短码
	TTL    int    `json:"ttl_seconds,omitempty"` // 可选过期秒数
}

type CreateResp struct {
	Code     string `json:"code"`
	ShortURL string `json:"short_url"`
	URL      string `json:"url"`
}

var db *gorm.DB

func main() {
	// 初始化 Gin
	r := gin.Default()
	// 允许跨域（仅示例）
	r.Use(cors.New(cors.Config{
		AllowAllOrigins:  true,
		AllowMethods:     []string{"GET", "POST", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// 初始化数据库（SQLite 单文件）
	initDB()

	// 路由
	// 需要 token 的接口
	api := r.Group("/api", tokenAuthMiddleware())
	{
		api.POST("/shorten", handleCreate)
		api.GET("/info/:code", handleInfo)
		api.DELETE("/:code", handleDelete)
	}
	// 重定向（放在路由末尾以避免冲突）
	r.GET("/:code", handleRedirect)

	port := os.Getenv("PORT")
	if port == "" {
		port = DefaultPort
	}
	log.Printf("短链接服务启动，端口 %s\n", port)
	r.Run(":" + port)
}

func initDB() {
	var err error
	db, err = gorm.Open(sqlite.Open("shortlink.db"), &gorm.Config{})
	if err != nil {
		log.Fatal("数据库打开失败：", err)
	}
	// 自动迁移
	err = db.AutoMigrate(&ShortLink{})
	if err != nil {
		log.Fatal("数据库迁移失败：", err)
	}
}

// ----------------- Token 验证中间件 -----------------
func tokenAuthMiddleware() gin.HandlerFunc {
	token := os.Getenv("API_TOKEN")
	if token == "" {
		log.Fatal("请设置环境变量 API_TOKEN")
	}
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少或无效的 Authorization header"})
			c.Abort()
			return
		}
		providedToken := strings.TrimPrefix(authHeader, "Bearer ")
		if providedToken != token {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "无效 token"})
			c.Abort()
			return
		}
		c.Next()
	}
}

// 处理创建短链
func handleCreate(c *gin.Context) {
	var req CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体解析失败"})
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url 不能为空"})
		return
	}
	// 验证 URL
	if !isValidURL(req.URL) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url 格式不正确"})
		return
	}

	// 处理自定义短码
	code := ""
	if req.Custom != "" {
		if len(req.Custom) > MaxCustomLength {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("自定义短码长度不能超过 %d", MaxCustomLength)})
			return
		}
		if !isValidCode(req.Custom) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "自定义短码只允许字母数字和 - _"})
			return
		}
		// 检查是否已存在
		var existing ShortLink
		res := db.Where("code = ?", req.Custom).First(&existing)
		if res.Error == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "自定义短码已存在"})
			return
		}
		code = req.Custom
	} else {
		// 生成随机短码（避免冲突）
		var err error
		for i := 0; i < 5; i++ { // 最多尝试 5 次
			code, err = genRandomCode(DefaultCodeLen)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "生成短码失败"})
				return
			}
			// 检查冲突
			var existing ShortLink
			res := db.Where("code = ?", code).First(&existing)
			if errors.Is(res.Error, gorm.ErrRecordNotFound) {
				break
			}
			// 如果存在，继续循环生成
			if i == 4 {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "生成短码冲突，稍后重试"})
				return
			}
		}
	}

	// 记录到 DB
	link := ShortLink{
		Code:      code,
		TargetURL: req.URL,
		CreatedAt: time.Now(),
	}
	if req.TTL > 0 {
		link.ExpiredAt = sql.NullTime{Valid: true, Time: time.Now().Add(time.Duration(req.TTL) * time.Second)}
	}

	if err := db.Create(&link).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存失败"})
		return
	}

	shortDomain := os.Getenv(DomainEnv)
	if shortDomain == "" {
		shortDomain = DefaultDomain
	}
	resp := CreateResp{
		Code:     code,
		ShortURL: strings.TrimRight(shortDomain, "/") + "/" + code,
		URL:      req.URL,
	}
	c.JSON(http.StatusOK, resp)
}

// 重定向处理
func handleRedirect(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的短码"})
		return
	}

	var link ShortLink
	res := db.Where("code = ?", code).First(&link)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "短链不存在"})
		return
	}
	// 检查是否过期
	if link.ExpiredAt.Valid && time.Now().After(link.ExpiredAt.Time) {
		c.JSON(http.StatusGone, gin.H{"error": "短链已过期"})
		return
	}

	// 增加点击量（异步/事务可改进，但这里直接更新）
	db.Model(&link).UpdateColumn("clicks", gorm.Expr("clicks + ?", 1))

	// 使用 302 临时重定向
	c.Redirect(http.StatusFound, link.TargetURL)
}

// 查询信息
func handleInfo(c *gin.Context) {
	code := c.Param("code")
	var link ShortLink
	res := db.Where("code = ?", code).First(&link)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "短链不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code":       link.Code,
		"url":        link.TargetURL,
		"clicks":     link.Clicks,
		"created_at": link.CreatedAt,
		"expired_at": link.ExpiredAt,
	})
}

// 删除短链
func handleDelete(c *gin.Context) {
	code := c.Param("code")
	res := db.Where("code = ?", code).Delete(&ShortLink{})
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "短链不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ------------------ 工具函数 ------------------

// 简单 URL 验证
func isValidURL(u string) bool {
	// 如果没有 scheme，则默认为 http
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	parsed, err := url.ParseRequestURI(u)
	if err != nil {
		return false
	}
	// 仅允许 http/https
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return true
}

var codeRegexp = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func isValidCode(s string) bool {
	return codeRegexp.MatchString(s)
}

// 生成随机短码（base62）
const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func genRandomCode(length int) (string, error) {
	if length <= 0 {
		length = DefaultCodeLen
	}
	b := make([]byte, length)
	// 使用 crypto/rand
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b), nil
}

// 可选：把数字转换成 base62（备用函数）
func encodeBase62(num uint64) string {
	if num == 0 {
		return "0"
	}
	var chars []byte
	base := uint64(len(alphabet))
	for num > 0 {
		rem := num % base
		chars = append([]byte{alphabet[rem]}, chars...)
		num = num / base
	}
	return string(chars)
}

// 生成分布相对均匀的随机数（用于可选的基于时间+随机的短码）
func randUint64() uint64 {
	var b [8]byte
	_, err := rand.Read(b[:])
	if err != nil {
		// fallback
		return uint64(time.Now().UnixNano())
	}
	return binary.LittleEndian.Uint64(b[:])
}

// 计算目标长度 (示例)
func ceilLogBase(n uint64, base float64) int {
	if n == 0 {
		return 1
	}
	return int(math.Ceil(math.Log(float64(n)) / math.Log(base)))
}

// -------------- 可选：导出 DB 内容为 JSON（便于备份/迁移） --------------
func exportDBToJSON(path string) error {
	var links []ShortLink
	if err := db.Find(&links).Error; err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(links)
}

// -------------- 简单测试函数（仅演示，不在生产环境使用） --------------
func seedExample() {
	// 如果表为空，插入示例数据
	var count int64
	db.Model(&ShortLink{}).Count(&count)
	if count == 0 {
		db.Create(&ShortLink{Code: "hello", TargetURL: "https://example.com", CreatedAt: time.Now()})
	}
}
