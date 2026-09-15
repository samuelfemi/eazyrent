package favorite

import "errors"

var (
	ErrAlreadyFavorited = errors.New("already favorited")
	ErrNotFavorited     = errors.New("not favorited")
)
