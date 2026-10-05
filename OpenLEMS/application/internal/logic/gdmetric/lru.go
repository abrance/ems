package gdmetric

import (
	"strconv"
	"strings"
	"unicode"
)

const defaultMaxSize = 32 << 20

type lruNode struct {
	date       string
	curve      DayCurve
	bytes      int64
	prev, next *lruNode
}

type curveLRU struct {
	max   int64
	used  int64
	n     int
	items map[string]*lruNode
	head  *lruNode
	tail  *lruNode
}

func newCurveLRU(max int64) *curveLRU {
	if max <= 0 {
		max = defaultMaxSize
	}
	return &curveLRU{max: max, items: map[string]*lruNode{}}
}

func curveBytes(c DayCurve) int64 {
	n := len(c.Load) + len(c.DayAhead) + len(c.Realtime)
	return int64(n)*48 + 96
}

func (l *curveLRU) Get(date string) (DayCurve, bool) {
	if l == nil {
		return DayCurve{}, false
	}
	node, ok := l.items[date]
	if !ok {
		return DayCurve{}, false
	}
	l.moveToHead(node)
	return node.curve, true
}

func (l *curveLRU) Put(date string, curve DayCurve) {
	if l == nil {
		return
	}
	size := curveBytes(curve)
	if node, ok := l.items[date]; ok {
		l.used -= node.bytes
		node.curve = curve
		node.bytes = size
		l.used += size
		l.moveToHead(node)
		l.evict()
		return
	}
	node := &lruNode{date: date, curve: curve, bytes: size}
	l.items[date] = node
	l.n++
	l.used += size
	l.pushHead(node)
	l.evict()
}

func (l *curveLRU) Len() int {
	if l == nil {
		return 0
	}
	return l.n
}

func (l *curveLRU) Used() int64 {
	if l == nil {
		return 0
	}
	return l.used
}

func (l *curveLRU) pushHead(node *lruNode) {
	node.prev = nil
	node.next = l.head
	if l.head != nil {
		l.head.prev = node
	}
	l.head = node
	if l.tail == nil {
		l.tail = node
	}
}

func (l *curveLRU) moveToHead(node *lruNode) {
	if l.head == node {
		return
	}
	l.detach(node)
	l.pushHead(node)
}

func (l *curveLRU) detach(node *lruNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		l.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		l.tail = node.prev
	}
	node.prev = nil
	node.next = nil
}

func (l *curveLRU) evict() {
	for l.used > l.max && l.n > 1 && l.tail != nil {
		l.drop(l.tail)
	}
}

func (l *curveLRU) drop(node *lruNode) {
	l.detach(node)
	delete(l.items, node.date)
	l.used -= node.bytes
	l.n--
}

func parseSize(raw string) int64 {
	s := strings.TrimSpace(strings.ToUpper(raw))
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return defaultMaxSize
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "GIB") || strings.HasSuffix(s, "GB"):
		mult = 1 << 30
		s = strings.TrimSuffix(strings.TrimSuffix(s, "GIB"), "GB")
	case strings.HasSuffix(s, "MIB") || strings.HasSuffix(s, "MB"):
		mult = 1 << 20
		s = strings.TrimSuffix(strings.TrimSuffix(s, "MIB"), "MB")
	case strings.HasSuffix(s, "KIB") || strings.HasSuffix(s, "KB"):
		mult = 1 << 10
		s = strings.TrimSuffix(strings.TrimSuffix(s, "KIB"), "KB")
	case strings.HasSuffix(s, "G"):
		mult = 1 << 30
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		mult = 1 << 20
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		mult = 1 << 10
		s = strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "B"):
		s = strings.TrimSuffix(s, "B")
	}
	s = strings.TrimRightFunc(s, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' })
	if s == "" {
		return defaultMaxSize
	}
	if strings.Contains(s, ".") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || f <= 0 {
			return defaultMaxSize
		}
		n := int64(f * float64(mult))
		if n <= 0 {
			return defaultMaxSize
		}
		return n
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return defaultMaxSize
	}
	if mult == 1 && n < 1024 {
		return n << 20
	}
	return n * mult
}
