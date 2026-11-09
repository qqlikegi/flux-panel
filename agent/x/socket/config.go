package socket

import (
	"os"

	"github.com/qqlikegi/forwardz/agent/x/config"
)

func saveConfig() {

	file := "forwardz.json"

	f, err := os.Create(file)
	if err != nil {
		return
	}
	defer f.Close()

	if err := config.Global().Write(f, "json"); err != nil {

		return
	}

	return
}
