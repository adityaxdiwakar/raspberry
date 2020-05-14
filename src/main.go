package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/google/go-querystring/query"
	"github.com/gorilla/websocket"
)

// CredentialsJSON stores credentials from TDA API
type CredentialsJSON struct {
	AuthToken           string `json:"authToken"`
	UserID              string `json:"userId"`
	UserCdDomainID      string `json:"userCdDomainId"`
	PrimaryAccountID    string `json:"primaryAccountId"`
	LastLoginTime       string `json:"lastLoginTime"`
	TokenExpirationTime string `json:"tokenExpirationTime"`
	LoginTime           string `json:"loginTime"`
	AccessLevel         string `json:"accessLevel"`
	StalePassword       bool   `json:"stalePassword"`
	StreamerInfo        struct {
		StreamerBinaryURL string `json:"streamerBinaryUrl"`
		StreamerSocketURL string `json:"streamerSocketUrl"`
		Token             string `json:"token"`
		TokenTimestamp    string `json:"tokenTimestamp"`
		UserGroup         string `json:"userGroup"`
		AccessLevel       string `json:"accessLevel"`
		ACL               string `json:"acl"`
		AppID             string `json:"appId"`
	} `json:"streamerInfo"`
	ProfessionalStatus string `json:"professionalStatus"`
	Quotes             struct {
		IsNyseDelayed   bool `json:"isNyseDelayed"`
		IsNasdaqDelayed bool `json:"isNasdaqDelayed"`
		IsOpraDelayed   bool `json:"isOpraDelayed"`
		IsAmexDelayed   bool `json:"isAmexDelayed"`
		IsCmeDelayed    bool `json:"isCmeDelayed"`
		IsIceDelayed    bool `json:"isIceDelayed"`
		IsForexDelayed  bool `json:"isForexDelayed"`
	} `json:"quotes"`
	StreamerSubscriptionKeys struct {
		Keys []struct {
			Key string `json:"key"`
		} `json:"keys"`
	} `json:"streamerSubscriptionKeys"`
	Accounts []struct {
		AccountID         string `json:"accountId"`
		DisplayName       string `json:"displayName"`
		AccountCdDomainID string `json:"accountCdDomainId"`
		Company           string `json:"company"`
		Segment           string `json:"segment"`
		ACL               string `json:"acl"`
		Authorizations    struct {
			Apex               bool   `json:"apex"`
			LevelTwoQuotes     bool   `json:"levelTwoQuotes"`
			StockTrading       bool   `json:"stockTrading"`
			MarginTrading      bool   `json:"marginTrading"`
			StreamingNews      bool   `json:"streamingNews"`
			OptionTradingLevel string `json:"optionTradingLevel"`
			StreamerAccess     bool   `json:"streamerAccess"`
			AdvancedMargin     bool   `json:"advancedMargin"`
			ScottradeAccount   bool   `json:"scottradeAccount"`
		} `json:"authorizations"`
	} `json:"accounts"`
}

// ParsedCreds to feed into socket
type ParsedCreds struct {
	Userid      string `json:"userid"`
	Token       string `json:"token"`
	Company     string `json:"company"`
	Segment     string `json:"segment"`
	Cddomain    string `json:"cddomain"`
	Usergroup   string `json:"usergroup"`
	Accesslevel string `json:"accesslevel"`
	Authorized  string `json:"authorized"`
	Timestamp   int64  `json:"timestamp"`
	Appid       string `json:"appid"`
	ACL         string `json:"acl"`
}

// WSRequest : data type to send in WS Channel
type WSRequest struct {
	Service    string        `json:"service"`
	Command    string        `json:"command"`
	Requestid  string        `json:"requestid"`
	Account    string        `json:"account"`
	Source     string        `json:"source"`
	Parameters RequestParams `json:"parameters"`
}

// RequestParams has params for WSRequest
type RequestParams struct {
	Credential string `json:"credential"`
	Token      string `json:"token"`
	Version    string `json:"version"`
	Keys       string `json:"keys"`
	Fields     string `json:"fields"`
}

// EncapsulatedRequest : formatted data to send in WS channel
type EncapsulatedRequest struct {
	Request []WSRequest `json:"requests"`
}

func convertCreds(creds CredentialsJSON) ParsedCreds {
	tsLayout := "2006-01-02T15:04:05-0700"
	t, err := time.Parse(tsLayout, creds.StreamerInfo.TokenTimestamp)
	if err != nil {
		log.Fatalf("An error occured parsing the timestamp: %v", err)
	}
	tMs := t.UnixNano() / (1000 * 1000)
	return ParsedCreds{
		Userid:      creds.Accounts[0].AccountID,
		Token:       creds.StreamerInfo.Token,
		Company:     creds.Accounts[0].Company,
		Segment:     creds.Accounts[0].Segment,
		Cddomain:    creds.Accounts[0].AccountCdDomainID,
		Usergroup:   creds.StreamerInfo.UserGroup,
		Accesslevel: creds.StreamerInfo.AccessLevel,
		Authorized:  "Y",
		Timestamp:   tMs,
		Appid:       creds.StreamerInfo.AppID,
		ACL:         creds.StreamerInfo.ACL,
	}
}

func main() {
	dat, err := os.Open("creds.json")
	if err != nil {
		log.Fatalf("An error occured opening credentials: %v", err)
	}
	log.Println("Successfully opened credentials.")

	defer dat.Close()

	byteValue, err := ioutil.ReadAll(dat)

	var creds CredentialsJSON
	json.Unmarshal(byteValue, &creds)

	parsedCreds := convertCreds(creds)

	credsQuery, err := query.Values(parsedCreds)
	credsStr := credsQuery.Encode()
	// credsStr, err := json.Marshal(parsedCreds)
	if err != nil {
		log.Fatalf("Could not serialize credentials: %v", err)
	}

	loginRequest := WSRequest{
		Service:   "ADMIN",
		Requestid: "1",
		Command:   "LOGIN",
		Account:   creds.Accounts[0].AccountID,
		Source:    creds.StreamerInfo.AppID,
		Parameters: RequestParams{
			Credential: credsStr,
			Token:      creds.StreamerInfo.Token,
			Version:    "1.0",
		},
	}

	quoteRequest := WSRequest{
		Service:   "QUOTE",
		Requestid: "2",
		Command:   "SUBS",
		Account:   creds.Accounts[0].AccountID,
		Source:    creds.StreamerInfo.AppID,
		Parameters: RequestParams{
			Keys:   "$ADVN, $DECN, $UVOL, $DVOL, $ADSPD, $ADQDC, $VOLSPD, $TICK, $TRIN, $VOLD, $ADD, $PCSP, VIX",
			Fields: "3",
		},
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	addr := flag.String("addr", creds.StreamerInfo.StreamerSocketURL, "http service address")
	u := url.URL{
		Scheme: "wss",
		Host:   *addr,
		Path:   "/ws",
	}

	log.Printf("Connecting to TDAmeritrade Socket")

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	} else {
		log.Printf("Received 'Hello' from WS")
	}
	defer c.Close()

	loginString, err := json.Marshal(loginRequest)
	if err != nil {
		log.Printf("Could not serialize request: %v", err)
	}

	quoteString, err := json.Marshal(quoteRequest)
	if err != nil {
		log.Printf("Could not serialize request: %v", err)
	}

	// c.WriteMessage(websocket.TextMessage, loginMessage)
	c.WriteMessage(websocket.TextMessage, loginString)

	done := make(chan struct{})

	count := 0
	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Fatal("read:", err)
				return
			}
			count += 1
			if count == 1 {
				c.WriteMessage(websocket.TextMessage, quoteString)
			}
			log.Printf("%s", message)
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case _ = <-ticker.C:
			// err := c.WriteMessage(websocket.TextMessage, []byte("[]"))
			// if err != nil {
			// log.Println("write:", err)
			// return
			// }
		case <-interrupt:
			log.Println("interrupt")

			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("write close:", err)
				return
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
