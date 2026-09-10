// Copyright 2026 Crosscue Ltd
// SPDX-License-Identifier: Apache-2.0

// List connected IDS cameras without initializing them.
package main

import (
	"context"
	"errors"
	"github.com/crosscue/go-imx636"
	"github.com/crosscue/go-imx636/examples/internal/example"
)

func main() {
	example.Main(func(ctx context.Context) error {
		cameras, err := imx636.List(ctx)
		return errors.Join(err, example.JSON(cameras))
	})
}
