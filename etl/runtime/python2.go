// Package runtime provides skeletons and static specifications for building ETL from scratch.
/*
 * Copyright (c) 2018-2021, NVIDIA CORPORATION. All rights reserved.
 */
package runtime

import (
	"strings"
)

type (
	// py2 implements Runtime for "python2".
	py2 struct{}
)

func (py2) Type() string        { return Python2 }
func (py2) CodeEnvName() string { return "AISTORE_CODE" }
func (py2) DepsEnvName() string { return "AISTORE_DEPS" }
func (py2) PodSpec() string     { return strings.ReplaceAll(pyPodSpec, "<VERSION>", "2") }
