package tools

import (
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
)

func InitLog(verbose bool) *logrus.Logger {
	var log = logrus.New()
	if verbose {
		// You could set this to any `io.Writer` such as a file
		file, err := os.OpenFile("log-tomd.log", os.O_CREATE|os.O_WRONLY, 0666)
		log.SetLevel(logrus.DebugLevel)
		if err == nil {
			log.SetOutput(file)
		} else {
			log.Info("Failed to log to file, using default stderr")
		}
	} else {
		//log.SetLevel(logrus.InfoLevel)
		logrus.SetOutput(ioutil.Discard)
	}
	return log
}
