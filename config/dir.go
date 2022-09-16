package config

import "github.ibm.com/distributed-trust-research/scalable-committer/utils"

//DirPath is needed to ensure consistent directory paths between the compiled (./scalable-committer/config/) and the binary version (./).
//An alternative solution would be to guarantee some env var that points to the root of the project every time.
//runtime.Caller(1) can guarantee consistent dir path as long as we are not compiling the code into a binary. Then the directory structure is not maintained.
//os.Getwd always returns the working directory that returns different paths depending on which file was run (e.g. from a test, a main.go file, a binary that has no directory structure).
var DirPath = utils.CurrentDir()
