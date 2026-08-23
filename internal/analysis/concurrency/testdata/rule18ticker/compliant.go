package rule18ticker

import "time"

func Compliant() {
	t := time.NewTimer(time.Second)
	defer t.Stop()
}

type owner struct{ ticker *time.Ticker }

func (o *owner) Start() {
	o.ticker = time.NewTicker(time.Second)
}

func OneShot() {
	_ = time.NewTimer(time.Second)
}
