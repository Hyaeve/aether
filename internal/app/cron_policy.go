package app

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type scheduledCronParser struct{}

var cronPolicyCache = struct {
	sync.Mutex
	items map[string]error
}{items: make(map[string]error)}

func (scheduledCronParser) Parse(expression string) (cron.Schedule, error) {
	schedule, err := rawCronParser.Parse(expression)
	if err != nil {
		return nil, errors.New("Cron格式无效，需要五个字段")
	}
	key := time.Local.String() + "\x00" + expression
	cronPolicyCache.Lock()
	result, found := cronPolicyCache.items[key]
	cronPolicyCache.Unlock()
	if !found {
		result = checkCronInterval(expression, 15*time.Minute, false)
		cronPolicyCache.Lock()
		if len(cronPolicyCache.items) >= 256 {
			clear(cronPolicyCache.items)
		}
		cronPolicyCache.items[key] = result
		cronPolicyCache.Unlock()
	}
	if result != nil {
		return nil, result
	}
	return schedule, nil
}

func (a *App) cronPolicyAPI(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Expression string `json:"expression"`
		Retry      bool   `json:"retry"`
	}
	if !decode(w, r, &input) {
		return
	}
	message := ""
	if len(input.Expression) > 128 {
		message = "Cron表达式最多128字节"
	} else if input.Expression != "" {
		_, err := cronParser.Parse(input.Expression)
		if err == nil && input.Retry {
			err = validateRapid115RetryCron(input.Expression)
		}
		if err != nil {
			message = err.Error()
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, 200, map[string]string{"error": message})
}
