package main

import (
	"encoding/gob"
	"flag"
	"log"
	"os"

	"github.com/nareix/mp4"
	mpegts "github.com/nareix/ts"
)

var (
	VideoWidth  int
	VideoHeight int
)

type GobAllSamples struct {
	TimeScale int
	SPS       []byte
	PPS       []byte
	Samples   []GobSample
}

type GobSample struct {
	Duration int
	Data     []byte
	Sync     bool
}

func main() {
	var saveGob bool
	var url string
	var maxgop int

	// with aac rtsp://admin:123456@80.254.21.110:554/mpeg4cif
	// with aac rtsp://admin:123456@95.31.251.50:5050/mpeg4cif
	// 1808p rtsp://admin:123456@171.25.235.18/mpeg4
	// 640x360 rtsp://admin:123456@94.242.52.34:5543/mpeg4cif

	flag.BoolVar(&saveGob, "s", false, "save to gob file")
	flag.IntVar(&maxgop, "g", 10, "max gop recording")
	flag.StringVar(&url, "url", "rtsp://admin:123456@176.99.65.80:558/mpeg4cif", "")
	flag.Parse()

	RtspReader := RtspClientNew()

	quit := false

	sps := []byte{}
	pps := []byte{}
	fuBuffer := []byte{}
	syncCount := 0

	// rtp timestamp: 90 kHz clock rate
	// 1 sec = timestamp 90000
	timeScale := 90000

	type NALU struct {
		ts   int
		data []byte
		sync bool
	}
	var lastNALU *NALU

	var mp4w *mp4.SimpleH264Writer
	var tsw *mpegts.SimpleH264Writer

	var allSamples *GobAllSamples

	outfileMp4, _ := os.Create("out.mp4")
	outfileTs, _ := os.Create("out.ts")
	outfileAAC, _ := os.Create("out.aac")
	endWriteNALU := func() {
		log.Println("finish write")
		if mp4w != nil {
			if err := mp4w.Finish(); err != nil {
				panic(err)
			}
		}
		outfileTs.Close()

		if saveGob {
			file, _ := os.Create("out.gob")
			enc := gob.NewEncoder(file)
			enc.Encode(allSamples)
			file.Close()
		}
	}

	writeNALU := func(sync bool, ts int, payload []byte) {
		if saveGob && allSamples == nil {
			allSamples = &GobAllSamples{
				SPS:       sps,
				PPS:       pps,
				TimeScale: timeScale,
			}
		}
		if mp4w == nil {
			mp4w = &mp4.SimpleH264Writer{
				SPS:       sps,
				PPS:       pps,
				TimeScale: timeScale,
				W:         outfileMp4,
			}
			//log.Println("SPS:\n"+hex.Dump(sps), "\nPPS:\n"+hex.Dump(pps))
		}
		curNALU := &NALU{
			ts:   ts,
			sync: sync,
			data: payload,
		}

		if lastNALU != nil {
			log.Println("write", lastNALU.sync, len(lastNALU.data))

			if err := mp4w.WriteNALU(lastNALU.sync, curNALU.ts-lastNALU.ts, lastNALU.data); err != nil {
				panic(err)
			}

			if tsw == nil {
				tsw = &mpegts.SimpleH264Writer{
					TimeScale: timeScale,
					W:         outfileTs,
					SPS:       sps,
					PPS:       pps,
					PCR:       int64(lastNALU.ts),
					PTS:       int64(lastNALU.ts),
				}
			}
			if err := tsw.WriteNALU(lastNALU.sync, curNALU.ts-lastNALU.ts, lastNALU.data); err != nil {
				panic(err)
			}

			if saveGob {
				allSamples.Samples = append(allSamples.Samples, GobSample{
					Sync:     lastNALU.sync,
					Duration: curNALU.ts - lastNALU.ts,
					Data:     lastNALU.data,
				})
			}
		}
		lastNALU = curNALU
	}

	handleNALU := func(nalType byte, payload []byte, ts int64) {
		if nalType == 7 {
			if len(sps) == 0 {
				sps = payload
			}
		} else if nalType == 8 {
			if len(pps) == 0 {
				pps = payload
			}
		} else if nalType == 5 {
			// keyframe
			syncCount++
			if syncCount == maxgop {
				quit = true
			}
			writeNALU(true, int(ts), payload)
		} else {
			// non-keyframe
			if syncCount > 0 {
				writeNALU(false, int(ts), payload)
			}
		}
	}

	if status, message := RtspReader.Client(url); status {
		log.Println("connected")
		i := 0
		for {
			i++
			//read 100 frame and exit loop
			if quit {
				break
			}
			select {
			case data := <-RtspReader.outgoing:

				if true {
					log.Printf("packet [0]=%x type=%d\n", data[0], data[1])
				}

				//log.Println("packet recive")
				if data[0] == 36 && data[1] == 0 {
					cc := data[4] & 0xF
					//rtp header
					rtphdr := 12 + cc*4

					//packet time
					ts := (int64(data[8]) << 24) + (int64(data[9]) << 16) + (int64(data[10]) << 8) + (int64(data[11]))

					//packet number
					packno := (int64(data[6]) << 8) + int64(data[7])
					if false {
						log.Println("packet num", packno)
					}

					nalType := data[4+rtphdr] & 0x1F

					if nalType >= 1 && nalType <= 23 {
						handleNALU(nalType, data[4+rtphdr:], ts)
					} else if nalType == 28 {
						isStart := data[4+rtphdr+1]&0x80 != 0
						isEnd := data[4+rtphdr+1]&0x40 != 0
						nalType := data[4+rtphdr+1] & 0x1F
						//nri := (data[4+rtphdr+1]&0x60)>>5
						nal := data[4+rtphdr]&0xE0 | data[4+rtphdr+1]&0x1F
						if isStart {
							fuBuffer = []byte{0}
						}
						fuBuffer = append(fuBuffer, data[4+rtphdr+2:]...)
						if isEnd {
							fuBuffer[0] = nal
							handleNALU(nalType, fuBuffer, ts)
						}
					}

				} else if data[0] == 36 && data[1] == 2 {
					// audio

					cc := data[4] & 0xF
					rtphdr := 12 + cc*4
					//or not payload := data[4+rtphdr:]
					payload := data[4+rtphdr+4:]
					outfileAAC.Write(payload)
					//log.Print("audio payload\n", hex.Dump(payload))
				}

			case <-RtspReader.signals:
				log.Println("exit signal by class rtsp")
			}
		}
	} else {
		log.Println("error", message)
	}

	endWriteNALU()
	RtspReader.Close()
}
