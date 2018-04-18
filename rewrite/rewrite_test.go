// Copyright (c) 2018 Timo Savola. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rewrite

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"testing"

	"github.com/tsavola/wag"
)

func TestEntryFunction(t *testing.T) {
	f, err := os.Open("testdata/entry-function.wasm")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	buf := new(bytes.Buffer)

	if err := EntryFunction(buf, bufio.NewReader(f), "_start", "main", []uint64{0, 0}); err != nil {
		t.Fatal(err)
	}

	if filename := os.Getenv("WAG_TEST_DUMP"); filename != "" {
		if err := ioutil.WriteFile(filename, buf.Bytes(), 0600); err != nil {
			t.Error(err)
		}
	}

	m := &wag.Module{
		EntrySymbol: "_start",
	}

	env := new(envImports)

	if err := m.Load(buf, env, new(bytes.Buffer), nil, 65536, nil); err != nil {
		t.Fatal(err)
	}

	// t.Logf("module = %v", m)
	// t.Logf("env = %v", env)
}
