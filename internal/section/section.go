// Copyright (c) 2016 Timo Savola. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package section

import (
	"io"
	"io/ioutil"

	"github.com/tsavola/wag/internal/errutil"
	"github.com/tsavola/wag/internal/loader"
	"github.com/tsavola/wag/internal/module"
)

// CopySpecific section if there is one.  Unknown sections preceding the
// desired section are silently discarded.  If another known section type is
// found, it is left untouched (the reader will be backed up before the section
// id).
func CopySpecific(w io.Writer, r module.Reader, sectionType byte) (ok bool, err error) {
	defer func() {
		err = errutil.ErrorOrPanic(recover())
	}()

	ok = copyPanic(w, r, sectionType)
	return
}

func copyPanic(w io.Writer, r module.Reader, expectId byte) (ok bool) {
	store := storer{w}
	load := loader.L{Reader: r}

loop:
	for {
		id, err := load.ReadByte()
		if err != nil {
			if err == io.EOF {
				return
			}
			panic(err)
		}

		switch {
		case id == module.SectionUnknown:
			payloadLen := load.Varuint32()
			if _, err := io.CopyN(ioutil.Discard, load, int64(payloadLen)); err != nil {
				panic(err)
			}

		case id == expectId:
			store.Byte(id)
			break loop

		default:
			load.UnreadByte()
			return
		}
	}

	payloadLen := load.Varuint32()
	store.Varuint32(payloadLen)

	if _, err := io.CopyN(store, load, int64(payloadLen)); err != nil {
		panic(err)
	}

	ok = true
	return
}
