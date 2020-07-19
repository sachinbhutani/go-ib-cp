package goibcp

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

//Version - the version of go-ib-cp
const Version = "0.0.2"

//ERROR, WARNING or INFO constants for Log Levels
const (
	ERROR   = 0
	WARNING = 1
	INFO    = 2
	DEBUG   = 3
)

//Config to connect to CP Web gateway
//LogInfo 0=> Log Errors only , 1=> log warnings, 2=> log information (default)
type Config struct {
	CPURL      string
	LogLevel   int
	AutoTickle bool
}

//Settings - Default settings if no setting are provided to the Connect() function.
var Settings = &Config{CPURL: "https://localhost:5000", LogLevel: 2, AutoTickle: true}

//Client - IB Client which can be used to call all api functions
var Client IBClient

//User - IBUser
var User IBUser
var rClient = resty.New()

//Connect to CP Web gateway.
//Returns a ib api client if successful or an error if connection is not established.
//If a session is established, auto-trickle function would be triggered to keep the session active using trciker api every minute.
func Connect(userSettings ...*Config) (*IBClient, error) {
	//Overwrite default settings if settings are provided.
	if len(userSettings) > 0 {
		if userSettings[0].CPURL != "" {
			Settings.CPURL = userSettings[0].CPURL
		}
		if userSettings[0].LogLevel != 2 {
			Settings.LogLevel = userSettings[0].LogLevel
		}
		if userSettings[0].AutoTickle == false { // default is true, but if user provides false the set autotickle to false.
			Settings.AutoTickle = userSettings[0].AutoTickle
		}

	}

	//ValidateSSO
	err := Client.GetEndpoint("sessionValidateSSO", &User)
	if err != nil {
		logMsg(ERROR, "Connect", "Failed to validate SSO", err)
		return &Client, err
	}
	//Get client authentication status, if client is not authenticate, attemp to re-authenticate 1 time.
	for i := 0; i < 2; i++ {
		err = Client.SessionStatus()
		if err != nil {
			logMsg(ERROR, "Connect", "Failed to validate SSO", err)
			return &Client, err
		}
		// if status is not connected, return error.
		if Client.IsConnected == false {
			logMsg(ERROR, "Connect", "Not connected to gateway, please login to CP web gateway again")
			return &Client, errors.New("Not connected to gateway, please login to CP web gateway again")
		}
		// if status is connected, but not authenticated, try to reauthenticate once.
		if Client.IsAuthenticated == false {
			err = Client.PostEndpoint("sessionReauthenticate", &IBClient{})
			if err != nil {
				logMsg(ERROR, "Connect", "Not able to re-authenticate with the gateway..quitting now")
				return &Client, err
			}
			time.Sleep(3 * time.Second)
			continue
		} else {
			if Settings.AutoTickle == true {
				go AutoTickle(&Client)
			}
			//TODO: trigger auto tickle
			return &Client, nil
		}
	}
	fmt.Printf("GOIBCP Client: %+v", Client)
	return &Client, nil
}

//PlaceOrder - posts and order
func (c *IBClient) PlaceOrder(order IBOrder) (IBOrderReply, error) {
	//Get Trading Account
	var orderReply IBOrderReply
	selectedAccount, err := c.GetSelectedAccount()
	if err != nil || selectedAccount == "" {
		logMsg(ERROR, "PlaceOrder", "Not able to find selected Trade account", err)
		return nil, err
	}
	epURL := Settings.CPURL + endpoints["orderPlace"]
	req := rClient.R().SetPathParams(map[string]string{"accountId": selectedAccount}).SetHeader("Content-Type", "application/json")
	req = req.SetBody(order).SetResult(&orderReply)
	resp, err := req.Post(epURL)
	if err != nil {
		logMsg(ERROR, "PlaceOrder", "Failed to post", err)
		return nil, err
	}
	logMsg(INFO, "PlaceOrder", resp.String())
	return orderReply, nil
}

//GetLiveOrders - Update live order to the order list struct
func (c *IBClient) GetLiveOrders(liveOrders *IBLiveOrders) error {
	return c.GetEndpoint("ordersLive", liveOrders)
}

//GetTradeAccount Get TradeAccount Information for the current trade account
func (c *IBClient) GetTradeAccount(res interface{}) error {
	return c.GetEndpoint("accountIserver", res)
}

//GetSelectedAccount - Get selected trade account ID , returns accountID as string or an error
func (c *IBClient) GetSelectedAccount() (string, error) {
	var tradeAccount IBTradeAccount
	err := c.GetEndpoint("accountIserver", &tradeAccount)
	if err != nil {
		logMsg(ERROR, "GetSelectedAccount", "Could not get Iserver trade account info", err)
		return "", err
	}
	return tradeAccount.SelectedAccount, nil
}

//GetPortfolioAccount - Gets the portfolio
//TODO: gets only a single account , may not work for multiple accounts
func (c *IBClient) GetPortfolioAccount() (string, error) {
	var portfolioAccounts IBPortfolioAccounts
	err := c.GetEndpoint("portfolioAccounts", &portfolioAccounts)
	if err != nil {
		logMsg(ERROR, "GetPortfolioAccount", "Could not get portfolio account ", err)
		return "", err
	}
	return portfolioAccounts[0].AccountID, nil
}

//GetPortfolioPositions - Get current open positions for an account
//Its required to call portfolio accounts before getting open positions, so account would be determined based on 1st account in portfolio accounts
//TODO: may not work for multiple accounts/subaccounts situations
func (c *IBClient) GetPortfolioPositions(openPositions *IBPortfolioPositions, pageID int) error {
	accountID, err := c.GetPortfolioAccount()
	if err != nil {
		logMsg(ERROR, "GetPortfolioPositions", "Could not get portfolio account ", err)
		return err
	}
	epURL := Settings.CPURL + endpoints["portfolioPositions"]
	req := rClient.R().SetPathParams(map[string]string{"accountId": accountID, "pageId": strconv.Itoa(pageID)})
	fmt.Println(req.URL)
	//req = req.SetResult(openPositions)
	resp, err := req.Get(epURL)
	if err != nil {
		logMsg(ERROR, "GetPortfolioPositions", "Failed to get portfolio positions", err)
		return err
	}
	logMsg(INFO, "GetPortfolioPositions", resp.String())
	return nil
}

//Tickle - Keeps the sesssion alive by tickeling the server, should be called by user application if autoTickle if off
func (c *IBClient) Tickle() error {
	var reply IBUser
	var err error
	err = c.GetEndpoint("sessionValidateSSO", &reply)
	logMsg(INFO, "Tickle", fmt.Sprintf("%+v", reply))
	if err != nil {
		return err
	}
	if reply.Expires == 0 {
		err = errors.New("Session Expired")
		return err
	}
	return nil
}

//Logout - Logout the current session
func (c *IBClient) Logout() error {
	var reply IBLogout
	var err error
	err = c.GetEndpoint("sessionLogout", &reply)
	logMsg(INFO, "Logout", fmt.Sprintf("%+v", reply))
	if err != nil {
		return err
	}
	return nil
}

//Reauthenticate - Reauthenticate existing client
func (c *IBClient) Reauthenticate() error {
	err := Client.PostEndpoint("sessionReauthenticate", &IBClient{})
	if err != nil {
		logMsg(ERROR, "Reauthenticate", "Not able to re-authenticate with the gateway..quitting now")
		return err
	}
	return nil
}

//SessionStatus - Returns session status
func (c *IBClient) SessionStatus() error {
	statusURL := Settings.CPURL + endpoints["sessionStatus"]
	_, err := rClient.R().SetResult(c).Get(statusURL)
	if err != nil {
		logMsg(ERROR, "SessionStatus", "Error getting session status", err)
		return err
	}
	logMsg(INFO, "SessionStatus:", fmt.Sprintf("%+v", c))
	return nil
}
