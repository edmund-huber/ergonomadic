package irc

import (
	"database/sql"
	"errors"
	"log"
	"regexp"
	"strings"
)

var (
	ErrNickMissing      = errors.New("nick missing")
	ErrNicknameInUse    = errors.New("nickname in use")
	ErrNicknameMismatch = errors.New("nickname mismatch")
	wildMaskExpr        = regexp.MustCompile(`\*|\?`)
	likeQuoter          = strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
		`*`, `%`,
		`?`, `_`)
)

func HasWildcards(mask string) bool {
	return wildMaskExpr.MatchString(mask)
}

func ExpandUserHost(userhost Name) (expanded Name) {
	expanded = userhost
	// fill in missing wildcards for nicks
	if !strings.Contains(expanded.String(), "!") {
		expanded += "!*"
	}
	if !strings.Contains(expanded.String(), "@") {
		expanded += "@*"
	}
	return
}

func QuoteLike(userhost Name) string {
	return likeQuoter.Replace(userhost.String())
}

type ClientLookupSet struct {
	byNick map[Name]*Client
	db     *ClientDB
}

func NewClientLookupSet(db string) *ClientLookupSet {
	return &ClientLookupSet{
		byNick: make(map[Name]*Client),
		db:     NewClientDB(db),
	}
}

func (clients *ClientLookupSet) Get(nick Name) *Client {
	return clients.byNick[nick.ToLower()]
}

func (clients *ClientLookupSet) Add(client *Client) error {
	if !client.HasNick() {
		return ErrNickMissing
	}
	if clients.Get(client.nick) != nil {
		return ErrNicknameInUse
	}
	clients.byNick[client.Nick().ToLower()] = client
	clients.db.Add(client)
	return nil
}

func (clients *ClientLookupSet) Remove(client *Client) error {
	if !client.HasNick() {
		return ErrNickMissing
	}
	if clients.Get(client.nick) != client {
		return ErrNicknameMismatch
	}
	delete(clients.byNick, client.nick.ToLower())
	clients.db.Remove(client)
	return nil
}

func (clients *ClientLookupSet) FindAll(userhost Name) (set ClientSet) {
	userhost = ExpandUserHost(userhost)
	set = make(ClientSet)
	rows, err := clients.db.db.Query(
		`SELECT nickname FROM client WHERE userhost LIKE ? ESCAPE '\'`,
		QuoteLike(userhost))
	if err != nil {
		Log.error.Println("ClientLookupSet.FindAll.Query:", err)
		return
	}
	for rows.Next() {
		var sqlNickname string
		err := rows.Scan(&sqlNickname)
		if err != nil {
			Log.error.Println("ClientLookupSet.FindAll.Scan:", err)
			return
		}
		nickname := Name(sqlNickname)
		client := clients.Get(nickname)
		if client == nil {
			Log.error.Println("ClientLookupSet.FindAll: missing client:", nickname)
			continue
		}
		set.Add(client)
	}
	return
}

func (clients *ClientLookupSet) Find(userhost Name) *Client {
	userhost = ExpandUserHost(userhost)
	row := clients.db.db.QueryRow(
		`SELECT nickname FROM client WHERE userhost LIKE ? ESCAPE '\' LIMIT 1`,
		QuoteLike(userhost))
	var nickname Name
	err := row.Scan(&nickname)
	if err != nil {
		Log.error.Println("ClientLookupSet.Find:", err)
		return nil
	}
	return clients.Get(nickname)
}

//
// client db
//

type ClientDB struct {
	db *sql.DB
}

func NewClientDB(db_path string) *ClientDB {
	db := &ClientDB{
		db: OpenDB(db_path),
	}
	stmts := []string{
		`CREATE TABLE client (
          nickname TEXT NOT NULL COLLATE NOCASE UNIQUE,
          userhost TEXT NOT NULL COLLATE NOCASE,
          UNIQUE (nickname, userhost) ON CONFLICT REPLACE)`,
		`CREATE UNIQUE INDEX idx_nick ON client (nickname COLLATE NOCASE)`,
		`CREATE UNIQUE INDEX idx_uh ON client (userhost COLLATE NOCASE)`,
	}
	for _, stmt := range stmts {
		_, err := db.db.Exec(stmt)
		if err != nil && !strings.HasSuffix(err.Error(), "already exists") {
			log.Fatal("NewClientDB: ", stmt, err)
		}
	}
	return db
}

func (db *ClientDB) Add(client *Client) {
	_, err := db.db.Exec(`INSERT INTO client (nickname, userhost) VALUES (?, ?)`,
		client.Nick().String(), client.UserHost().String())
	if err != nil {
		Log.error.Println("ClientDB.Add:", err)
	}
}

func (db *ClientDB) Remove(client *Client) {
	_, err := db.db.Exec(`DELETE FROM client WHERE nickname = ?`,
		client.Nick().String())
	if err != nil {
		Log.error.Println("ClientDB.Remove:", err)
	}
}

//
// usermask to regexp
//

type UserMaskSet struct {
	masks  map[Name]bool
	regexp *regexp.Regexp
}

func NewUserMaskSet() *UserMaskSet {
	return &UserMaskSet{
		masks: make(map[Name]bool),
	}
}

func (set *UserMaskSet) Add(mask Name) bool {
	if set.masks[mask] {
		return false
	}
	set.masks[mask] = true
	set.setRegexp()
	return true
}

func (set *UserMaskSet) AddAll(masks []Name) (added bool) {
	for _, mask := range masks {
		if !added && !set.masks[mask] {
			added = true
		}
		set.masks[mask] = true
	}
	set.setRegexp()
	return
}

func (set *UserMaskSet) Remove(mask Name) bool {
	if !set.masks[mask] {
		return false
	}
	delete(set.masks, mask)
	set.setRegexp()
	return true
}

func (set *UserMaskSet) Match(userhost Name) bool {
	if set.regexp == nil {
		return false
	}
	return set.regexp.MatchString(userhost.String())
}

func (set *UserMaskSet) String() string {
	masks := make([]string, len(set.masks))
	index := 0
	for mask := range set.masks {
		masks[index] = mask.String()
		index += 1
	}
	return strings.Join(masks, " ")
}

// Generate a regular expression from the set of user mask
// strings. Masks are split at the two types of wildcards, `*` and
// `?`. All the pieces are meta-escaped. `*` is replaced with `.*`,
// the regexp equivalent. Likewise, `?` is replaced with `.`. The
// parts are re-joined and finally all masks are joined into a big
// or-expression.
func (set *UserMaskSet) setRegexp() {
	if len(set.masks) == 0 {
		set.regexp = nil
		return
	}

	maskExprs := make([]string, len(set.masks))
	index := 0
	for mask := range set.masks {
		manyParts := strings.Split(mask.String(), "*")
		manyExprs := make([]string, len(manyParts))
		for mindex, manyPart := range manyParts {
			oneParts := strings.Split(manyPart, "?")
			oneExprs := make([]string, len(oneParts))
			for oindex, onePart := range oneParts {
				oneExprs[oindex] = regexp.QuoteMeta(onePart)
			}
			manyExprs[mindex] = strings.Join(oneExprs, ".")
		}
		maskExprs[index] = strings.Join(manyExprs, ".*")
	}
	expr := "^" + strings.Join(maskExprs, "|") + "$"
	set.regexp, _ = regexp.Compile(expr)
}
