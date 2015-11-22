package main

import (
	"log"
	"os"
)

func main() {
	RtspReader := RtspClientNew()
	//wait keyframe
	wait := true
	//sps string
	sps := ""
	//pps string
	pps := ""
	//bufer string from test
	bufer := ""
	//create file
	file, err := os.Create("test.h264") // For read access.
	if err != nil {
		log.Fatal(err)
	}
	if status, message := RtspReader.Client("rtsp://admin:123456@171.25.235.18/mpeg4"); status {
		log.Println("connected")
		i := 0
		for {
			i++
			//read 100 frame and exit loop
			if i == 100 {
				break
			}
			select {
			case data := <-RtspReader.outgoing:
				//log.Println("packet recive")
				if data[0] == 36 && data[1] == 0 {
					cc := data[4] & 0xF
					//rtp header
					rtphdr := 12 + cc*4
					//packet time
					ts := (int64(data[8]) << 24) + (int64(data[9]) << 16) + (int64(data[10]) << 8) + (int64(data[11]))

					//packet number
					packno := (int64(data[6]) << 8) + int64(data[7])
					log.Println("packet num", packno)
					nalType := data[4+rtphdr] & 0x1F
					//nal
					nal := data[4+rtphdr]&0xE0 | data[4+rtphdr+1]&0x1F
					//srart fragment packet
					pstart := data[4+rtphdr+1]&0x80 != 0
					//end fragment packet
					pend := data[4+rtphdr+1]&0x40 != 0
					if nalType == 7 {
						sps = string(data[4+rtphdr:])
					} else if nalType == 8 {
						pps = string(data[4+rtphdr:])
						wait = false
					} else if nalType == 28 {
						if !wait {
							if pstart {
								bufer = bufer + string(data[4+rtphdr+2:])
							} else if pend {
								bufer = bufer + string(data[4+rtphdr+2:])
								if nal == 101 {
									//write key frame h264 format
									log.Println("packet ts", ts)
									file.Write([]byte("\000\000\001" + sps + "\000\000\001" + pps + "\000\000\001" + string(nal) + bufer))
								} else {
									//write not key frame h264 format
									log.Println("packet ts", ts)
									file.Write([]byte("\000\000\001" + string(nal) + bufer))
								}
								bufer = ""
							} else {
								bufer = bufer + string(data[4+rtphdr+2:])
							}
						}
					}
				}
			case <-RtspReader.signals:
				log.Println("exit signal by class rtsp")
			}
		}
	} else {
		log.Println("error", message)
		return
	}
	RtspReader.Close()
	file.Close()
}
