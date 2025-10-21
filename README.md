# shortlink
Go短链生成服务，Go + Gin + GORM + SQLite（单文件，方便演示和快速启动）

短链接服务（单文件示例）
功能：
 - POST /api/shorten  创建短链接（支持自定义短链接）
 - GET /:code         重定向到原始长链接，并统计点击量
 - GET /api/info/:code 查询短链接信息（原始链接、点击量、创建时间）
 - DELETE /api/:code   删除短链接


运行：
 1. 安装依赖：
    go mod init shortlink && go mod tidy
 2. 启动：
    go run main.go
 3. 示例请求：
    curl -X POST -H "Content-Type: application/json" -d '{"url":"https://example.com/very/long/link"}' http://localhost:8080/api/shorten
 4. 或者自定义短码：
    curl -X POST -H "Content-Type: application/json" -d '{"url":"https://example.com","custom":"go123"}' http://localhost:8080/api/shorten