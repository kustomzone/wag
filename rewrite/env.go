// Copyright (c) 2018 Timo Savola. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rewrite

import (
	"github.com/tsavola/wag/types"
)

type envFunction struct {
	module string
	field  string
	sig    types.Function
}

type envGlobal struct {
	module string
	field  string
	t      types.T
}

type envImports struct {
	funcs   []envFunction
	globals []envGlobal
}

func (env *envImports) ImportFunction(module, field string, sig types.Function) (bool, uint64, error) {
	env.funcs = append(env.funcs, envFunction{module, field, sig})
	return false, 0, nil
}

func (env *envImports) ImportGlobal(module, field string, t types.T) (uint64, error) {
	env.globals = append(env.globals, envGlobal{module, field, t})
	return 0, nil
}
