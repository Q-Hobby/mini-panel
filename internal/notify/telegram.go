package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var tokenRe = regexp.MustCompile(`^[0-9]{5,15}:[A-Za-z0-9_-]{30,80}$`)

// ValidToken accepts the token @BotFather gives.
func ValidToken(t string) error {
	if !tokenRe.MatchString(t) {
		return errors.New("this is not a bot token: it looks like 123456789:AA... and comes from @BotFather")
	}
	return nil
}

// Bot talks to the Telegram Bot API.
type Bot struct {
	Token string
	API   string // https://api.telegram.org when empty
	HTTP  *http.Client
}

// ErrConflict is Telegram saying another program reads the messages of the bot: a second hub, or a webhook.
var ErrConflict = errors.New("another program reads the messages of this bot")

// ErrUnauthorized is Telegram refusing the token.
var ErrUnauthorized = errors.New("Telegram does not accept this token")

type apiAnswer struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// RetryError is Telegram asking to wait before the next message.
type RetryError struct{ After time.Duration }

func (e RetryError) Error() string { return fmt.Sprintf("Telegram asks to wait %s", e.After) }

func (b Bot) call(ctx context.Context, method string, params url.Values, out any) error {
	api := b.API
	if api == "" {
		api = "https://api.telegram.org"
	}
	c := b.HTTP
	if c == nil {
		c = &http.Client{Timeout: 70 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", api+"/bot"+b.Token+"/"+method, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.Do(req)
	if err != nil {
		// The address holds the token; the error of the client repeats the address.
		return errNetwork{strings.ReplaceAll(err.Error(), b.Token, "<token>")}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var a apiAnswer
	if err := json.Unmarshal(raw, &a); err != nil {
		return fmt.Errorf("Telegram answered %d with something that is not JSON", resp.StatusCode)
	}
	if !a.OK {
		switch {
		case a.ErrorCode == 401 || a.ErrorCode == 404:
			return ErrUnauthorized
		case a.ErrorCode == 409:
			return ErrConflict
		case a.ErrorCode == 429:
			return RetryError{time.Duration(max(a.Parameters.RetryAfter, 1)) * time.Second}
		}
		return fmt.Errorf("Telegram: %s", a.Description)
	}
	if out != nil {
		return json.Unmarshal(a.Result, out)
	}
	return nil
}

// Me returns the user name of the bot, which also proves the token works.
func (b Bot) Me(ctx context.Context) (string, error) {
	var me struct {
		Username string `json:"username"`
	}
	if err := b.call(ctx, "getMe", url.Values{}, &me); err != nil {
		return "", err
	}
	return me.Username, nil
}

// Send sends a message in HTML (see Escape). A connection that breaks is tried once more after two seconds (seen on Linode: a reset on
// the first connection to api.telegram.org, then nothing); an answer of Telegram is never sent twice.
func (b Bot) Send(ctx context.Context, chat int64, text string) error {
	params := url.Values{"chat_id": {strconv.FormatInt(chat, 10)}, "text": {text}, "parse_mode": {"HTML"}, "disable_web_page_preview": {"true"}}
	err := b.call(ctx, "sendMessage", params, nil)
	var net errNetwork
	if errors.As(err, &net) {
		select {
		case <-ctx.Done():
			return err
		case <-time.After(2 * time.Second):
		}
		err = b.call(ctx, "sendMessage", params, nil)
	}
	return err
}

// errNetwork is a failure before Telegram answered.
type errNetwork struct{ msg string }

func (e errNetwork) Error() string { return e.msg }

// Update is a message sent to the bot.
type Update struct {
	ID      int64
	ChatID  int64
	Title   string // the name of the chat, or of the person
	Text    string
	Private bool
}

// Updates waits up to wait for messages after offset (long polling).
func (b Bot) Updates(ctx context.Context, offset int64, wait time.Duration) ([]Update, error) {
	var raw []struct {
		UpdateID int64 `json:"update_id"`
		Message  *struct {
			Text string `json:"text"`
			Chat struct {
				ID        int64  `json:"id"`
				Type      string `json:"type"`
				Title     string `json:"title"`
				Username  string `json:"username"`
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
			} `json:"chat"`
		} `json:"message"`
	}
	err := b.call(ctx, "getUpdates", url.Values{"offset": {strconv.FormatInt(offset, 10)}, "timeout": {strconv.Itoa(int(wait.Seconds()))},
		"allowed_updates": {`["message"]`}}, &raw)
	if err != nil {
		return nil, err
	}
	out := make([]Update, 0, len(raw))
	for _, r := range raw {
		u := Update{ID: r.UpdateID}
		if m := r.Message; m != nil {
			u.ChatID, u.Text, u.Private = m.Chat.ID, m.Text, m.Chat.Type == "private"
			u.Title = m.Chat.Title
			if u.Title == "" {
				u.Title = strings.TrimSpace(m.Chat.FirstName + " " + m.Chat.LastName)
			}
			if m.Chat.Username != "" {
				u.Title = strings.TrimSpace(u.Title + " @" + m.Chat.Username)
			}
		}
		out = append(out, u)
	}
	return out, nil
}

// Command is the command of a message, without the bot's name: "/status@mybot x" is "/status".
func Command(text string) string {
	f := strings.Fields(text)
	if len(f) == 0 || !strings.HasPrefix(f[0], "/") {
		return ""
	}
	c, _, _ := strings.Cut(f[0], "@")
	return strings.ToLower(c)
}

// Escape makes text safe inside a message in HTML.
func Escape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// Split cuts a long message at line ends, so each part fits the 4096 characters Telegram takes.
func Split(text string) []string {
	const limit = 3800
	var parts []string
	var cur bytes.Buffer
	for _, line := range strings.SplitAfter(text, "\n") {
		if cur.Len()+len(line) > limit && cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
		if len(line) > limit {
			line = line[:limit]
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}
