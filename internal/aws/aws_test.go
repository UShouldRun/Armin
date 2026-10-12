package aws

import (
  "context"
  "io"
  "net/http"
  "net/http/httptest"
  "strings"
  "sync"
  "testing"
  "time"
)

var testCreds = Credentials{
  AccessKeyID:     "AKIDEXAMPLE",
  SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
}

var testTime = time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)

// Vectors from AWS's published Signature V4 test suite.
func TestSignMatchesAWSTestSuite(t *testing.T) {
  cases := []struct {
    name, url, signature string
  }{
    {
      "get-vanilla",
      "https://example.amazonaws.com/",
      "5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31",
    },
    {
      "get-vanilla-query-order-key-case",
      "https://example.amazonaws.com/?Param2=value2&Param1=value1",
      "b97d918cfa904a5beff61c982a1b6f458b799221646efd99d3219ec94cdf2500",
    },
  }

  for _, c := range cases {
    req, _ := http.NewRequest(http.MethodGet, c.url, nil)
    Sign(req, nil, testCreds, "us-east-1", "service", testTime)

    want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, " +
      "SignedHeaders=host;x-amz-date, Signature=" + c.signature
    got := req.Header.Get("Authorization")
    if got != want {
      t.Errorf("%s:\n got  %s\n want %s", c.name, got, want)
    }
  }
}

// fakeEC2 serves a tiny state machine for one instance.
type fakeEC2 struct {
  mu      sync.Mutex
  state   string
  actions []string
}

func (f *fakeEC2) handler(t *testing.T) http.HandlerFunc {
  return func(w http.ResponseWriter, r *http.Request) {
    if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/") {
      t.Errorf("unsigned request: %q", r.Header.Get("Authorization"))
    }

    raw, _ := io.ReadAll(r.Body)
    body := string(raw)

    f.mu.Lock()
    defer f.mu.Unlock()
    switch {
    case strings.Contains(body, "Action=DescribeInstances"):
      ip := ""
      if f.state == "running" {
        ip = "<ipAddress>203.0.113.7</ipAddress>"
      }

      io.WriteString(w, `<DescribeInstancesResponse><reservationSet><item><instancesSet><item>`+
        `<instanceId>i-1</instanceId><instanceState><code>0</code><name>`+f.state+`</name></instanceState>`+
        ip+`<privateIpAddress>10.0.0.5</privateIpAddress></item></instancesSet></item></reservationSet></DescribeInstancesResponse>`)
      if f.state == "pending" {
        f.state = "running"
      }
    case strings.Contains(body, "Action=StartInstances"):
      f.actions = append(f.actions, "start")
      f.state = "pending"
      io.WriteString(w, `<StartInstancesResponse/>`)
    case strings.Contains(body, "Action=StopInstances"):
      f.actions = append(f.actions, "stop")
      f.state = "stopping"
      io.WriteString(w, `<StopInstancesResponse/>`)
    case strings.Contains(body, "InstanceId.1=i-missing"):
      w.WriteHeader(400)
    }
  }
}

func TestWaitRunningStartsStoppedInstance(t *testing.T) {
  StatePoll = 10 * time.Millisecond
  f := &fakeEC2{ state: "stopped" }
  server := httptest.NewServer(f.handler(t))
  defer server.Close()

  ec2 := NewEC2("eu-west-1", testCreds)
  ec2.Endpoint = server.URL

  var seen []string
  ctx, cancel := context.WithTimeout(context.Background(), 20 * time.Second)
  defer cancel()
  inst, started, err := ec2.WaitRunning(ctx, "i-1", func(s string) { seen = append(seen, s) })
  if err != nil {
    t.Fatalf("WaitRunning: %v", err)
  }

  if !started || inst.Host() != "203.0.113.7" {
    t.Fatalf("started=%v host=%q", started, inst.Host())
  }

  if len(seen) < 2 || !strings.Contains(seen[0], "stopped") {
    t.Fatalf("status = %v", seen)
  }

  err = ec2.StopInstance(ctx, "i-1")
  if err != nil {
    t.Fatal(err)
  }

  if strings.Join(f.actions, ",") != "start,stop" {
    t.Fatalf("actions = %v", f.actions)
  }
}

func TestEC2ErrorIsReadable(t *testing.T) {
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(403)
    io.WriteString(w, `<Response><Errors><Error><Code>UnauthorizedOperation</Code>`+
      `<Message>You are not authorized</Message></Error></Errors></Response>`)
  }))
  defer server.Close()

  ec2 := NewEC2("eu-west-1", testCreds)
  ec2.Endpoint = server.URL
  err := ec2.StartInstance(context.Background(), "i-1")
  if err == nil || !strings.Contains(err.Error(), "UnauthorizedOperation: You are not authorized") {
    t.Fatalf("err = %v", err)
  }
}
