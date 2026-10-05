package gdmetric

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

func ingestFile(path string) ([]DayRow, map[string]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	br := bufio.NewReader(f)
	var pos int64
	if bom, peekErr := br.Peek(3); peekErr == nil && len(bom) >= 3 && bom[0] == 0xef && bom[1] == 0xbb && bom[2] == 0xbf {
		_, _ = br.Discard(3)
		pos += 3
	}

	accs := map[string]*dayAcc{}
	offsets := map[string]int64{}
	for {
		line, err := br.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, err
		}
		start := pos
		pos += int64(len(line))
		date, clock, typeID, unit, value, ok := parseMetricLine(line)
		if !ok {
			if err == io.EOF {
				break
			}
			continue
		}
		if _, exists := offsets[date]; !exists {
			offsets[date] = start
		}
		day := accs[date]
		if day == nil {
			day = &dayAcc{}
			accs[date] = day
		}
		bucket := day.bucket(typeID)
		if bucket == nil {
			if err == io.EOF {
				break
			}
			continue
		}
		bucket.add(clock, value, unit, false)
		if err == io.EOF {
			break
		}
	}
	return finishDays(accs), offsets, nil
}

func readDayCurve(path string, offset int64, date string) (DayCurve, error) {
	f, err := os.Open(path)
	if err != nil {
		return DayCurve{}, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return DayCurve{}, err
	}
	br := bufio.NewReader(f)
	day := &dayAcc{}
	for {
		line, err := br.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if err == io.EOF {
				break
			}
			return DayCurve{}, err
		}
		rowDate, clock, typeID, unit, value, ok := parseMetricLine(line)
		if ok {
			if rowDate != date {
				break
			}
			if bucket := day.bucket(typeID); bucket != nil {
				bucket.add(clock, value, unit, true)
			}
		}
		if err == io.EOF {
			break
		}
	}
	curve := DayCurve{Date: date}
	if day.load != nil {
		curve.Load = day.load.samples
	}
	if day.da != nil {
		curve.DayAhead = day.da.samples
	}
	if day.rt != nil {
		curve.Realtime = day.rt.samples
	}
	return curve, nil
}

func parseMetricLine(line []byte) (date, clock string, typeID int, unit string, value float64, ok bool) {
	line = bytes.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return "", "", 0, "", 0, false
	}
	reader := csv.NewReader(bytes.NewReader(line))
	reader.ReuseRecord = false
	reader.FieldsPerRecord = -1
	rec, err := reader.Read()
	if err != nil || len(rec) < 8 {
		return "", "", 0, "", 0, false
	}
	if len(rec[0]) > 0 {
		rec[0] = stripBOM(rec[0])
	}
	date = strings.TrimSpace(rec[1])
	clock = strings.TrimSpace(rec[2])
	typeID, err = strconv.Atoi(strings.TrimSpace(rec[3]))
	if err != nil || date == "" {
		return "", "", 0, "", 0, false
	}
	unit = strings.TrimSpace(rec[5])
	value, err = strconv.ParseFloat(strings.TrimSpace(rec[6]), 64)
	if err != nil {
		return "", "", 0, "", 0, false
	}
	return date, clock, typeID, unit, value, true
}

func finishDays(accs map[string]*dayAcc) []DayRow {
	dates := make([]string, 0, len(accs))
	for date := range accs {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	days := make([]DayRow, 0, len(dates))
	for _, date := range dates {
		d := accs[date]
		days = append(days, DayRow{
			Date:     date,
			Load:     d.load.stat(),
			DayAhead: d.da.stat(),
			Realtime: d.rt.stat(),
		})
	}
	return days
}
