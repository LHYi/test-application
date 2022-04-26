package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hyperledger/fabric-sdk-go/pkg/common/providers/fab"
	"github.com/hyperledger/fabric-sdk-go/pkg/core/config"
	"github.com/hyperledger/fabric-sdk-go/pkg/gateway"
)

// these address should be changed accordingly when implemented in the hardware
const (
	// the mspID should be identical to the one used when calling cryptogen to generate credential files
	// mspID = "Org1MSP"
	// the path of the certificates
	cryptoPath  = "../fabric-samples-2.3/test-network/organizations/peerOrganizations/org1.example.com"
	certPath    = cryptoPath + "/users/User1@org1.example.com/msp/signcerts/cert.pem"
	keyPath     = cryptoPath + "/users/User1@org1.example.com/msp/keystore/"
	tlsCertPath = cryptoPath + "/peers/peer0.org1.example.com/tls/ca.crt"
	// an IP address to access the peer node, it is a localhost address when the network is running in a single machine
	peerEndpoint = "localhost:7051"
	// name of the peer node
	gatewayPeer = "peer0.org1.example.com"
	// the channel name and the chaincode name should be identical to the ones used in blockchain network implementation, the following are the default values
	// these information have been designed to be entered by the application user
	networkName  = "mychannel"
	contractName = "basic"
	userName     = "appUser"
)

func main() {
	err := os.Setenv("DISCOVERY_AS_LOCALHOST", "true")
	if err != nil {
		log.Fatalf("Error setting DISCOVERY_AS_LOCALHOST environment variable: %v", err)
		os.Exit(1)
	}

	log.Println("============ Creating wallet ============")
	wallet, err := gateway.NewFileSystemWallet("wallet")
	if err != nil {
		log.Fatalf("Failed to create wallet: %v", err)
	}
	log.Println("============ Wallet created ============")

	if !wallet.Exists(userName) {
		err = populateWallet(wallet, userName)
		if err != nil {
			log.Fatalf("->Failed to populate wallet contents: %v", err)
		}
		log.Printf("-> Successfully add user %s to wallet \n", userName)
	} else {
		log.Printf("->  User %s already exists", userName)
	}

	ccpPath := filepath.Join(
		"..",
		"fabric-samples-2.3",
		"test-network",
		"organizations",
		"peerOrganizations",
		"org1.example.com",
		"connection-org1.yaml",
	)

	log.Println("============ connecting to gateway ============")
	gw, err := gateway.Connect(
		gateway.WithConfig(config.FromFile(filepath.Clean(ccpPath))),
		gateway.WithIdentity(wallet, userName),
	)
	if err != nil {
		log.Fatalf("Failed to connect to gateway: %v", err)
	}
	defer gw.Close()
	log.Println("============ Successfully connected to gateway ============")

	network, err := gw.GetNetwork("mychannel")
	if err != nil {
		log.Fatalf("Failed to get network: %v", err)
	}
	log.Println("============ successfully connected to network", networkName, "============")

	contract := network.GetContract(contractName)
	log.Println("============ successfully got contract", contractName, "============")

funcLoop:
	for {
		fmt.Println("-> Continue?: [y/n] ")
		continueConfirm := catchOneInput()
		if isYes(continueConfirm) {
			eventID := "Org1[a-zA-Z]+"
			reg, notifier, err := contract.RegisterEvent(eventID)
			if err != nil {
				fmt.Printf("Failed to register contract event: %s", err)
				return
			}
			defer contract.Unregister(reg)
			invokeFunc(contract)
			var event *fab.CCEvent
			select {
			case event = <-notifier:
				fmt.Printf("Received chaincode event: %s - %s \n", event.EventName, event.Payload)
			case <-time.After(time.Second * 1):
				fmt.Printf("No chaincode event\n")
			}
			contract.Unregister(reg)
		} else if isNo(continueConfirm) {
			break funcLoop
		} else {
			fmt.Println("Wrong input")
		}
	}

	// the credentials must be cleaned if you are going to shut down the current network connection
	// everytime the network is established, new credential files will be generated
	fmt.Println("-> Clean up?: [y/n] ")
	cleanUpConfirm := catchOneInput()
	if isYes(cleanUpConfirm) {
		cleanUp()
	}
}

func populateWallet(wallet *gateway.Wallet, userName string) error {
	credPath := filepath.Join(
		"..",
		"fabric-samples-2.3",
		"test-network",
		"organizations",
		"peerOrganizations",
		"org1.example.com",
		"users",
		"User1@org1.example.com",
		"msp",
	)

	certPath := filepath.Join(credPath, "signcerts", "User1@org1.example.com-cert.pem")
	// read the certificate pem
	cert, err := ioutil.ReadFile(filepath.Clean(certPath))
	if err != nil {
		return err
	}

	keyDir := filepath.Join(credPath, "keystore")
	// there's a single file in this dir containing the private key
	files, err := ioutil.ReadDir(keyDir)
	if err != nil {
		return err
	}
	if len(files) != 1 {
		return fmt.Errorf("keystore folder should have contain one file")
	}
	keyPath := filepath.Join(keyDir, files[0].Name())
	key, err := ioutil.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return err
	}

	identity := gateway.NewX509Identity("Org1MSP", string(cert), string(key))

	return wallet.Put(userName, identity)
}

func cleanUp() {
	log.Println("-> Cleaning up wallet...")
	if _, err := os.Stat("wallet"); err == nil {
		e := os.RemoveAll("wallet")
		if e != nil {
			log.Fatal(e)
		}
	}
	if _, err := os.Stat("keystore"); err == nil {
		e := os.RemoveAll("keystore")
		if e != nil {
			log.Fatal(e)
		}
	}
	log.Println("-> Wallet cleaned up successfully")
}

func invokeFunc(contract *gateway.Contract) {
	var functionName string
	var paraNumber int
	fmt.Println("-> Please enter the name of the smart contract function you want to invoke")
	functionName = catchOneInput()
	fmt.Println("-> Please enter the number of parameters")
	paraNumber, _ = strconv.Atoi(catchOneInput())
	var functionPara []string
	for i := 0; i < paraNumber; i++ {
		fmt.Printf("-> Please enter parameter %v: ", i+1)
		functionPara = append(functionPara, catchOneInput())
	}
	if paraNumber == 0 {
		result, err := contract.SubmitTransaction(functionName)
		if err != nil {
			panic(fmt.Errorf("failed to submit transaction: %w", err))
		}
		fmt.Printf("Result: %s \n", string(result))
	} else {
		result, err := contract.SubmitTransaction(functionName, functionPara...)
		if err != nil {
			panic(fmt.Errorf("failed to submit transaction: %w", err))
		}
		fmt.Printf("Result: %s \n", string(result))
	}
}

func catchOneInput() string {
	// instantiate a new reader
	reader := bufio.NewReader(os.Stdin)
	s, _ := reader.ReadString('\n')
	// get rid of the \n at the end of the string
	s = strings.Replace(s, "\n", "", -1)
	// if the string is exit, exit the application directly
	// this allows the user to exit the application whereever they want and saves the effort of detecting the exit command elsewhere
	if isExit(s) {
		exitApp()
	}
	return s
}

func isYes(s string) bool {
	return strings.Compare(s, "Y") == 0 || strings.Compare(s, "y") == 0 || strings.Compare(s, "Yes") == 0 || strings.Compare(s, "yes") == 0
}

func isNo(s string) bool {
	return strings.Compare(s, "N") == 0 || strings.Compare(s, "n") == 0 || strings.Compare(s, "No") == 0 || strings.Compare(s, "no") == 0
}

func isExit(s string) bool {
	return strings.Compare(s, "Exit") == 0 || strings.Compare(s, "exit") == 0 || strings.Compare(s, "EXIT") == 0
}

func exitApp() {
	log.Println("============ application-golang ends ============")
	// exit code zero indicates that no error occurred
	os.Exit(0)
}

func formatJSON(data []byte) string {
	var result bytes.Buffer
	if err := json.Indent(&result, data, "", "  "); err != nil {
		panic(fmt.Errorf("failed to parse JSON: %w", err))
	}
	return result.String()
}
