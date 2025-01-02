package tools

import (
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
)

func InitLogger(verbose bool) error {

	if verbose {
		// You could set this to any `io.Writer` such as a file
		file, err := os.OpenFile("log-tomd.log", os.O_CREATE|os.O_WRONLY, 0666)
		log.SetLevel(log.DebugLevel)
		if err == nil {
			log.SetOutput(file)
		} else {
			log.Info("Failed to log to file, using default stderr")
			return err
		}
	} else {
		log.SetOutput(ioutil.Discard)
	}
	return nil
}
