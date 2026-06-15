package umbra

import (
	"errors"
	"fmt"
)

// ErrorKind is a stable SDK-level error category.
type ErrorKind string

const (
	ErrInvalidParams      ErrorKind = "invalid_params"
	ErrInvalidToken       ErrorKind = "invalid_token"
	ErrForbidden          ErrorKind = "forbidden"
	ErrQuotaInsufficient  ErrorKind = "quota_insufficient"
	ErrFileNotFound       ErrorKind = "file_not_found"
	ErrFileAlreadyExists  ErrorKind = "file_already_exists"
	ErrUploadNotFound     ErrorKind = "upload_not_found"
	ErrStorageUnavailable ErrorKind = "storage_unavailable"
	ErrNetwork            ErrorKind = "network"
	ErrTimeout            ErrorKind = "timeout"
	ErrInternal           ErrorKind = "internal"
	ErrAuth               ErrorKind = "auth"
)

// UmbraError represents an SDK, protocol, or Umbra API error.
type UmbraError struct {
	Kind       ErrorKind
	HTTPStatus int
	Code       int
	Message    string
	Cause      error
}

func (e *UmbraError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	if e.Kind != "" {
		return string(e.Kind)
	}
	return "umbra error"
}

func (e *UmbraError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func apiError(status int, code int, msg string) *UmbraError {
	return &UmbraError{
		Kind:       kindForCode(status, code),
		HTTPStatus: status,
		Code:       code,
		Message:    msg,
	}
}

func wrapNetwork(err error) error {
	if err == nil {
		return nil
	}
	return &UmbraError{Kind: ErrNetwork, Message: err.Error(), Cause: err}
}

func invalidInput(format string, args ...any) error {
	return &UmbraError{Kind: ErrInvalidParams, Message: fmt.Sprintf(format, args...)}
}

func authError(format string, args ...any) error {
	return &UmbraError{Kind: ErrAuth, Message: fmt.Sprintf(format, args...)}
}

func kindForCode(status int, code int) ErrorKind {
	switch code {
	case 1001:
		return ErrInvalidParams
	case 1004:
		return ErrInvalidToken
	case 1005:
		return ErrForbidden
	case 2001:
		return ErrQuotaInsufficient
	case 2002:
		return ErrFileNotFound
	case 2010:
		return ErrUploadNotFound
	case 2005:
		return ErrStorageUnavailable
	case 5000:
		return ErrInternal
	}
	if status == 401 {
		return ErrInvalidToken
	}
	if status == 403 {
		return ErrForbidden
	}
	if status >= 500 {
		return ErrInternal
	}
	return ErrInvalidParams
}

func isInvalidToken(err error) bool {
	var ue *UmbraError
	if errors.As(err, &ue) {
		return ue.Kind == ErrInvalidToken || ue.HTTPStatus == 401 || ue.Code == 1004
	}
	return false
}
