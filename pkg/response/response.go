package response

import (
	"errors"
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// Response 统一 API 响应信封
// HTTP 状态码与业务 code 保持一致（如 400 → code:400）
// msg 字段统一使用英文，同时充当前端 i18n key
type Response struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Data    any    `json:"data,omitempty"`
	Errors  any    `json:"errors,omitempty"`   // 字段级校验错误
	TraceID string `json:"trace_id,omitempty"` // 请求追踪 ID
}

// PageData 标准分页数据结构
type PageData[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// OK 成功响应，HTTP 200 + code:0
func OK(c *gin.Context, data any) {
	c.JSON(200, Response{Code: 0, Msg: "ok", Data: data})
}

// Fail 通用失败响应，HTTP 状态码与业务 code 一致
func Fail(c *gin.Context, code int, msg string) {
	c.JSON(code, Response{Code: code, Msg: msg})
}

func BadRequest(c *gin.Context, msg string) { Fail(c, 400, msg) }

// ValidationError 字段级校验失败响应，HTTP 422 表示语义正确但数据不合法
func ValidationError(c *gin.Context, errs map[string]string) {
	c.JSON(422, Response{Code: 422, Msg: "validation failed", Errors: errs})
}

func Unauthorized(c *gin.Context)          { Fail(c, 401, "unauthorized") }
func Forbidden(c *gin.Context)            { Fail(c, 403, "forbidden") }
func NotFound(c *gin.Context, msg string) { Fail(c, 404, msg) }
func Timeout(c *gin.Context)              { Fail(c, 504, "timeout") }
func InternalError(c *gin.Context)        { Fail(c, 500, "internal error") }

// ParseValidationErrors 将 binding 错误解析为字段 → 消息的映射
// 非 validator.ValidationErrors 类型的错误（如 json.UnmarshalTypeError）会放入 "_" 键
func ParseValidationErrors(err error) map[string]string {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		result := make(map[string]string)
		for _, fe := range ve {
			result[fe.Field()] = formatValidationErr(fe)
		}
		return result
	}
	return map[string]string{"_": err.Error()}
}

func formatValidationErr(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", fe.Field())
	case "oneof":
		return fmt.Sprintf("must be one of %s", fe.Param())
	case "min":
		return fmt.Sprintf("minimum length/value is %s", fe.Param())
	case "max":
		return fmt.Sprintf("maximum length/value is %s", fe.Param())
	default:
		return fmt.Sprintf("validation failed on rule %s", fe.Tag())
	}
}
