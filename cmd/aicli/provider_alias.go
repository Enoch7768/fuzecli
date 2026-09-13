package main

import "os"

// provider_alias keeps the public CLI vocabulary friendly while preserving the
// existing interactive setup implementation. `aicli provider setup` behaves
// exactly like `aicli setup` without duplicating setup logic.
func init() {
	if len(os.Args) >= 3 && os.Args[1] == "provider" && os.Args[2] == "setup" {
		os.Args = append([]string{os.Args[0], "setup"}, os.Args[3:]...)
	}
}
