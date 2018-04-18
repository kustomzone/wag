// Copyright (c) 2016 Timo Savola. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package sections

import (
	"io"
	"io/ioutil"

	"github.com/tsavola/wag/internal/module"
	"github.com/tsavola/wag/internal/section"
)

// CopyCodeSection if there is one.  Unknown sections preceding the code
// section are silently discarded.  If another known section type is found, it
// is left untouched (the reader will be backed up before the section id).
func CopyCodeSection(w io.Writer, r module.Reader) (ok bool, err error) {
	return section.CopySpecific(w, r, module.SectionCode)
}

func DiscardUnknownSections(r module.Reader) (err error) {
	_, err = section.CopySpecific(ioutil.Discard, r, module.SectionUnknown)
	return
}
