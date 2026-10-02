package wordstat

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Sentinel-ошибки: потребитель по ним отличает сбой от «данных нет» и решает,
// имеет ли смысл повторять запрос. Оборачиваются через %w, поэтому проверяются
// через errors.Is.
var (
	// ErrInvalidArgument — некорректный запрос: невалидные параметры у нас или
	// INVALID_ARGUMENT (HTTP 400 / gRPC code 3) от API. Повтор не поможет.
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrQuotaExceeded — исчерпана квота (HTTP 429 / gRPC code 8). Повтор имеет смысл.
	ErrQuotaExceeded = errors.New("quota exceeded")
	// ErrUnavailable — временная недоступность: 5xx и сетевые сбои. Повтор имеет смысл.
	ErrUnavailable = errors.New("upstream unavailable")
	// ErrInternal — всё остальное (включая 401/403: проблема с доступом на нашей стороне).
	ErrInternal = errors.New("internal error")
)

// gRPC-коды google.rpc.Code, которые API кладёт в тело ошибки.
const (
	grpcInvalidArgument   = 3
	grpcResourceExhausted = 8
	grpcInternal          = 13
	grpcUnavailable       = 14
)

// apiFailure собирает ошибку с sentinel-обёрткой, сохраняя текст от API.
// HTTP-статус авторитетнее gRPC-кода; код используется, когда статус ничего
// не говорит (например, ошибка пришла внутри HTTP 200).
func apiFailure(status, grpcCode int, message string) error {
	sentinel := ErrInternal

	switch {
	case status == http.StatusBadRequest:
		sentinel = ErrInvalidArgument
	case status == http.StatusTooManyRequests:
		sentinel = ErrQuotaExceeded
	case status >= 500:
		sentinel = ErrUnavailable
	default:
		switch grpcCode {
		case grpcInvalidArgument:
			sentinel = ErrInvalidArgument
		case grpcResourceExhausted:
			sentinel = ErrQuotaExceeded
		case grpcInternal, grpcUnavailable:
			sentinel = ErrUnavailable
		}
	}

	if message == "" {
		return fmt.Errorf("%w: HTTP %d", sentinel, status)
	}
	return fmt.Errorf("%w: HTTP %d: %s", sentinel, status, message)
}

// apiErrorEnvelope разбирает конверт ошибки Yandex Cloud
// ({"code":3,"message":"rpc error: ..."}). Возвращает ok=false для обычных
// ответов и для пустого объекта.
func apiErrorEnvelope(data []byte) (code int, message string, ok bool) {
	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return 0, "", false
	}
	if envelope.Code == 0 && envelope.Message == "" {
		return 0, "", false
	}
	return envelope.Code, envelope.Message, true
}
