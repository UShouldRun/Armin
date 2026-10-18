package cloud

import (
  "net/http"
)

// awsCredentials are an access key pair, plus a session token for
// temporary credentials.
type awsCredentials struct {
  AccessKeyID, SecretAccessKey, SessionToken string
}

// ec2Client calls the EC2 Query API in one region.
type ec2Client struct {
  Region   string
  Creds    awsCredentials
  HTTP     *http.Client
  Endpoint string // tests override; default https://ec2.<region>.amazonaws.com
}

// ec2Instance is what DescribeInstance reports.
type ec2Instance struct {
  ID, State, PublicIP, PrivateIP, PublicDNS string
}

type ec2Error struct {
  Errors []struct {
    Code    string `xml:"Code"`
    Message string `xml:"Message"`
  } `xml:"Errors>Error"`
}

type describeResponse struct {
  Reservations []struct {
    Instances []struct {
      ID        string `xml:"instanceId"`
      State     string `xml:"instanceState>name"`
      PublicIP  string `xml:"ipAddress"`
      PrivateIP string `xml:"privateIpAddress"`
      PublicDNS string `xml:"dnsName"`
    } `xml:"instancesSet>item"`
  } `xml:"reservationSet>item"`
}
