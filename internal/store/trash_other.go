//go:build !windows && !darwin

package store

import "errors"

func trash([]string) error { return errors.ErrUnsupported }
