package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/hyperledger/fabric-gateway/pkg/client"
	"github.com/hyperledger/fabric-gateway/pkg/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// these address should be changed accordingly when implemented in the hardware
const (
	// the mspID should be identical to the one used when calling cryptogen to generate credential files
	mspID = "Org1MSP"
	// the path of the certificates
	cryptoPath  = "../fabric-samples-2.4/test-network/organizations/peerOrganizations/org1.example.com"
	certPath    = cryptoPath + "/users/User1@org1.example.com/msp/signcerts/User1@org1.example.com-cert.pem"
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
)

func main() {
	err := os.Setenv("DISCOVERY_AS_LOCALHOST", "true")
	if err != nil {
		log.Fatalf("Error setting DISCOVERY_AS_LOCALHOST environemnt variable: %v", err)
		os.Exit(1)
	}

	clientConnection := newGrpcConnection()
	defer clientConnection.Close()
	// generation of identity files
	id := newIdentity(mspID)
	sign := newSign()

	gateway, err := client.Connect(
		id,
		client.WithSign(sign),
		client.WithClientConnection(clientConnection),
		// Default timeouts for different gRPC calls
		client.WithEvaluateTimeout(5*time.Second),
		client.WithEndorseTimeout(15*time.Second),
		client.WithSubmitTimeout(5*time.Second),
		client.WithCommitStatusTimeout(1*time.Minute),
	)
	if err != nil {
		panic(err)
	}
	defer gateway.Close()

	network := gateway.GetNetwork(networkName)
	contract := network.GetContract(contractName)
	fmt.Println("Successfully connected to the network.")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startChaincodeEventListening(ctx, network)

funcLoop:
	for {
		fmt.Println("-> Continue?: [y/n] ")
		continueConfirm := catchOneInput()
		if isYes(continueConfirm) {
			invokeFunc(contract)
		} else if isNo(continueConfirm) {
			break funcLoop
		} else {
			fmt.Println("Wrong input")
		}
	}

	replayChaincodeEvents(ctx, network, 3)
}

func startChaincodeEventListening(ctx context.Context, network *client.Network) {
	fmt.Println("\n*** Start chaincode event listening")

	events, err := network.ChaincodeEvents(ctx, contractName)
	if err != nil {
		panic(fmt.Errorf("failed to start chaincode event listening: %w", err))
	}

	go func() {
		for event := range events {
			asset := event.Payload
			fmt.Printf("\n<-- Chaincode event received: %s - %s\n", event.EventName, asset)
		}
	}()
}

func replayChaincodeEvents(ctx context.Context, network *client.Network, startBlock uint64) {
	fmt.Println("\n*** Start chaincode event replay")

	events, err := network.ChaincodeEvents(ctx, contractName, client.WithStartBlock(startBlock))
	if err != nil {
		panic(fmt.Errorf("failed to start chaincode event listening: %w", err))
	}

	for {
		select {
		case <-time.After(10 * time.Second):
			panic(errors.New("timeout waiting for event replay"))

		case event := <-events:
			asset := event.Payload
			fmt.Printf("\n<-- Chaincode event replayed: %s - %s\n", event.EventName, asset)

			if event.EventName == "DeleteAsset" {
				// Reached the last submitted transaction so return to stop listening for events
				return
			}
		}
	}
}

func invokeFunc(contract *client.Contract) {
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
		result, commit, err := contract.SubmitAsync(functionName, client.WithArguments())
		if err != nil {
			panic(fmt.Errorf("failed to submit transaction: %w", err))
		}
		fmt.Printf("Result: %s \n", result)
		status, err := commit.Status()
		if err != nil {
			panic(err)
		}
		if !status.Successful {
			panic(fmt.Errorf("transaction %s failed to commit with status code %d", status.TransactionID, int32(status.Code)))
		}
	} else {
		result, commit, err := contract.SubmitAsync(functionName, client.WithArguments(functionPara...))
		if err != nil {
			panic(fmt.Errorf("failed to submit transaction: %w", err))
		}
		fmt.Printf("Result: %s \n", result)
		status, err := commit.Status()
		if err != nil {
			panic(err)
		}
		if !status.Successful {
			panic(fmt.Errorf("transaction %s failed to commit with status code %d", status.TransactionID, int32(status.Code)))
		}
	}

}

func newGrpcConnection() *grpc.ClientConn {
	certificate, err := loadCertificate(tlsCertPath)
	if err != nil {
		panic(err)
	}

	certPool := x509.NewCertPool()
	certPool.AddCert(certificate)
	transportCredentials := credentials.NewClientTLSFromCert(certPool, gatewayPeer)

	connection, err := grpc.Dial(peerEndpoint, grpc.WithTransportCredentials(transportCredentials))
	if err != nil {
		panic(fmt.Errorf("failed to create gRPC connection: %w", err))
	}

	return connection
}

func newIdentity(mspID string) *identity.X509Identity {
	certificate, err := loadCertificate(certPath)
	if err != nil {
		panic(err)
	}

	id, err := identity.NewX509Identity(mspID, certificate)
	if err != nil {
		panic(err)
	}

	return id
}

func loadCertificate(filename string) (*x509.Certificate, error) {
	certificatePEM, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}
	return identity.CertificateFromPEM(certificatePEM)
}

func newSign() identity.Sign {
	files, err := ioutil.ReadDir(keyPath)
	if err != nil {
		panic(fmt.Errorf("failed to read private key directory: %w", err))
	}
	privateKeyPEM, err := ioutil.ReadFile(path.Join(keyPath, files[0].Name()))

	if err != nil {
		panic(fmt.Errorf("failed to read private key file: %w", err))
	}

	privateKey, err := identity.PrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		panic(err)
	}

	sign, err := identity.NewPrivateKeySign(privateKey)
	if err != nil {
		panic(err)
	}

	return sign
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
