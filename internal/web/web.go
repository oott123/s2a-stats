// Package web 内嵌前端静态资源到二进制中。
package web

import _ "embed"

// IndexHTML 是单页前端的完整 HTML（含内联 CSS/JS）。
//
//go:embed index.html
var IndexHTML []byte
