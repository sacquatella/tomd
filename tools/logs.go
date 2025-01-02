package tools

import (
	logger "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
)

func InitLogger(verbose bool) error {

	if verbose {
		// You could set this to any `io.Writer` such as a file
		file, err := os.OpenFile("log-tomd.log", os.O_CREATE|os.O_WRONLY, 0666)
		logger.SetLevel(logger.DebugLevel)
		if err == nil {
			logger.SetOutput(file)
		} else {
			logger.Info("Failed to log to file, using default stderr")
			return err
		}
	} else {
		logger.SetOutput(ioutil.Discard)
	}
	return nil
}
