package aws

import (
  "net/http"
)

// Credentials are an access key pair, plus a session token for
// temporary credentials.
type Credentials struct {
  AccessKeyID, SecretAccessKey, SessionToken string
}

// EC2 calls the EC2 Query API in one region.
type EC2 struct {
  Region   string
  Creds    Credentials
  HTTP     *http.Client
  Endpoint string // tests override; default https://ec2.<region>.amazonaws.com
}

// Instance is what DescribeInstance reports.
type Instance struct {
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
