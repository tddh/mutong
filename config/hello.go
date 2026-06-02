package config

import (
	"log"
	"time"
)

var Mutong = `
     __  __      _   _      _____       ___       _   _       ____       
    |  \/  |    | | | |    |_   _|     / _ \     | \ | |     /.___|      
    | |\/| |    | | | |      | |      | | | |    |  \| |    | | _ _      
    | |  | |    | |_| |      | |      | |_| |    | |\  |    | |_| |      
    |_|  |_|     \___/       |_|       \___/     |_| \_|     \____|      

`

func init() {
	log.Print(Mutong)
	time.Sleep(1 * time.Second)
}
