// AWS: the slice armin needs to run a model on an EC2 instance it
// starts and stops itself: Signature V4 request signing and three EC2
// actions (describe, start, stop), on the standard library.
//
//   awsEnvCredentials  keys from the environment
//   awsSign            Signature V4 for one request
//   ec2Client          DescribeInstance, StartInstance, StopInstance,
//                      WaitRunning
//   ec2Machine         the Machine (cloud.go) for an instance

package cloud

import (
  "context"
  "crypto/hmac"
  "crypto/sha256"
  "encoding/hex"
  "encoding/xml"
  "errors"
  "fmt"
  "io"
  "net/http"
  "net/url"
  "os"
  "sort"
  "strings"
  "time"
)

// errNoAWSCredentials means neither the environment nor .env had keys.
var errNoAWSCredentials = errors.New(
  "no AWS credentials: set AWS_ACCESS_KEY_ID and " +
    "AWS_SECRET_ACCESS_KEY (e.g. in ~/.config/armin/.env)",
)

// awsCredentialEnv lists the environment variables credentials come
// from — also what a Manager must keep out of spawned processes.
var awsCredentialEnv = []string{
  "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
}

// awsEnvCredentials reads the standard AWS environment variables.
func awsEnvCredentials() (awsCredentials, error) {
  c := awsCredentials{
    AccessKeyID:     os.Getenv("AWS_ACCESS_KEY_ID"),
    SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
    SessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
  }

  if c.AccessKeyID == "" || c.SecretAccessKey == "" {
    return awsCredentials{}, errNoAWSCredentials
  }

  return c, nil
}

// --- Signature V4 ---

const (
  sigAlgorithm = "AWS4-HMAC-SHA256"
  amzDate      = "20060102T150405Z"
  shortDate    = "20060102"
)

func sha256Hex(b []byte) string {
  sum := sha256.Sum256(b)
  return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
  h := hmac.New(sha256.New, key)
  h.Write([]byte(data))

  return h.Sum(nil)
}

// awsSign adds x-amz-date (and the session token) and the
// Authorization header to req, signing body for region and service
// at time now. req.Host must already be set.
func awsSign(
  req *http.Request, body []byte, creds awsCredentials,
  region, service string, now time.Time,
) {
  now = now.UTC()
  stamp := now.Format(amzDate)
  day := now.Format(shortDate)

  req.Header.Set("X-Amz-Date", stamp)
  if creds.SessionToken != "" {
    req.Header.Set("X-Amz-Security-Token", creds.SessionToken)
  }

  // Canonical headers: host plus every header set on the request,
  // lowercased, sorted, values trimmed.
  headers := map[string]string{ "host": req.Host }
  for k, v := range req.Header {
    headers[strings.ToLower(k)] = strings.Join(v, ",")
  }

  names := make([]string, 0, len(headers))
  for k := range headers {
    names = append(names, k)
  }

  sort.Strings(names)

  var canonHeaders strings.Builder
  for _, k := range names {
    canonHeaders.WriteString(k)
    canonHeaders.WriteByte(':')
    canonHeaders.WriteString(strings.TrimSpace(headers[k]))
    canonHeaders.WriteByte('\n')
  }

  signed := strings.Join(names, ";")

  path := req.URL.EscapedPath()
  if path == "" {
    path = "/"
  }

  canonical := strings.Join([]string{
    req.Method,
    path,
    canonicalQuery(req.URL.Query()),
    canonHeaders.String(),
    signed,
    sha256Hex(body),
  }, "\n")

  scope := day + "/" + region + "/" + service + "/aws4_request"
  toSign := strings.Join([]string{
    sigAlgorithm, stamp, scope, sha256Hex([]byte(canonical)),
  }, "\n")

  key := hmacSHA256([]byte("AWS4" + creds.SecretAccessKey), day)
  key = hmacSHA256(key, region)
  key = hmacSHA256(key, service)
  key = hmacSHA256(key, "aws4_request")
  signature := hex.EncodeToString(hmacSHA256(key, toSign))

  req.Header.Set("Authorization", fmt.Sprintf(
    "%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
    sigAlgorithm, creds.AccessKeyID, scope, signed, signature,
  ))
}

// canonicalQuery sorts by key then value, with AWS's escaping
// (spaces as %20, not +).
func canonicalQuery(q url.Values) string {
  if len(q) == 0 {
    return ""
  }

  keys := make([]string, 0, len(q))
  for k := range q {
    keys = append(keys, k)
  }

  sort.Strings(keys)

  var parts []string
  for _, k := range keys {
    vals := append([]string{}, q[k]...)
    sort.Strings(vals)
    for _, v := range vals {
      parts = append(parts, awsEscape(k) + "=" + awsEscape(v))
    }
  }

  return strings.Join(parts, "&")
}

func awsEscape(s string) string {
  return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// --- EC2 ---

// ec2Version is the EC2 Query API version these calls are written
// against.
const ec2Version = "2016-11-15"

// newEC2 returns a client for region.
func newEC2(region string, creds awsCredentials) *ec2Client {
  return &ec2Client{
    Region: region,
    Creds:  creds,
    HTTP:   &http.Client{ Timeout: 30 * time.Second },
  }
}

// Host is the address to reach the instance at: public IP, then
// public DNS, then private IP (over a VPN).
func (i ec2Instance) Host() string {
  switch {
  case i.PublicIP != "":
    return i.PublicIP
  case i.PublicDNS != "":
    return i.PublicDNS
  }

  return i.PrivateIP
}

func (e *ec2Client) call(
  ctx context.Context, action string, params url.Values, out any,
) error {
  params.Set("Action", action)
  params.Set("Version", ec2Version)
  body := []byte(params.Encode())

  endpoint := e.Endpoint
  if endpoint == "" {
    endpoint = "https://ec2." + e.Region + ".amazonaws.com"
  }

  req, err := http.NewRequestWithContext(
    ctx, http.MethodPost, endpoint + "/", strings.NewReader(string(body)),
  )
  if err != nil {
    return fmt.Errorf("\n\t... in call:\n\t%w", err)
  }

  req.Header.Set(
    "Content-Type", "application/x-www-form-urlencoded; charset=utf-8",
  )
  awsSign(req, body, e.Creds, e.Region, "ec2", time.Now())

  resp, err := e.HTTP.Do(req)
  if err != nil {
    return fmt.Errorf("\n\t... in call:\n\t %s: %w", action, err)
  }

  defer resp.Body.Close()

  raw, err := io.ReadAll(io.LimitReader(resp.Body, 1 << 20))
  if err != nil {
    return fmt.Errorf("\n\t... in call:\n\t %s: %w", action, err)
  }

  if resp.StatusCode != http.StatusOK {
    var ae ec2Error
    if xml.Unmarshal(raw, &ae) == nil && len(ae.Errors) > 0 {
      return fmt.Errorf(
        "\n\t... in call:\n\t %s: %s: %s",
        action, ae.Errors[0].Code, ae.Errors[0].Message,
      )
    }

    return fmt.Errorf(
      "\n\t... in call:\n\t %s: HTTP %d: %s",
      action, resp.StatusCode, string(raw),
    )
  }

  if out == nil {
    return nil
  }

  err = xml.Unmarshal(raw, out)
  if err != nil {
    return fmt.Errorf("\n\t... in call:\n\t %s: %w", action, err)
  }

  return nil
}

// DescribeInstance reports one instance's state and addresses.
func (e *ec2Client) DescribeInstance(
  ctx context.Context, id string,
) (ec2Instance, error) {
  var out describeResponse
  err := e.call(
    ctx, "DescribeInstances", url.Values{ "InstanceId.1": { id } }, &out,
  )
  if err != nil {
    return ec2Instance{}, fmt.Errorf("\n\t... in DescribeInstance:\n\t%w", err)
  }

  for _, r := range out.Reservations {
    for _, i := range r.Instances {
      if i.ID == id {
        return ec2Instance{
          ID:        i.ID,
          State:     i.State,
          PublicIP:  i.PublicIP,
          PrivateIP: i.PrivateIP,
          PublicDNS: i.PublicDNS,
        }, nil
      }
    }
  }

  return ec2Instance{}, fmt.Errorf(
    "\n\t... in DescribeInstance:\n\t instance %s not found in %s",
    id, e.Region,
  )
}

// StartInstance asks EC2 to start id; it returns before it's running.
func (e *ec2Client) StartInstance(ctx context.Context, id string) error {
  err := e.call(
    ctx, "StartInstances", url.Values{ "InstanceId.1": { id } }, nil,
  )
  if err != nil {
    return fmt.Errorf("\n\t... in StartInstance:\n\t%w", err)
  }

  return nil
}

// StopInstance asks EC2 to stop id; it returns before it's stopped.
func (e *ec2Client) StopInstance(ctx context.Context, id string) error {
  err := e.call(
    ctx, "StopInstances", url.Values{ "InstanceId.1": { id } }, nil,
  )
  if err != nil {
    return fmt.Errorf("\n\t... in StopInstance:\n\t%w", err)
  }

  return nil
}

// WaitRunning polls until id is running with an address, starting it
// if it's stopped (and waiting out a stop in progress first).
// started reports whether this call started it.
func (e *ec2Client) WaitRunning(
  ctx context.Context, id string, status func(string),
) (inst ec2Instance, started bool, err error) {
  last := ""
  for {
    inst, err = e.DescribeInstance(ctx, id)
    if err != nil {
      return inst, started, fmt.Errorf("\n\t... in WaitRunning:\n\t%w", err)
    }

    if inst.State != last && status != nil {
      status(fmt.Sprintf("instance %s is %s", id, inst.State))
    }

    last = inst.State

    switch inst.State {
    case "running":
      if inst.Host() != "" {
        return inst, started, nil
      }
    case "stopped":
      err := e.StartInstance(ctx, id)
      if err != nil {
        return inst, started, fmt.Errorf("\n\t... in WaitRunning:\n\t%w", err)
      }

      started = true
    case "terminated", "shutting-down":
      return inst, started, fmt.Errorf(
        "\n\t... in WaitRunning:\n\t instance %s is %s", id, inst.State,
      )
    }

    select {
    case <-ctx.Done():
      return inst, started, fmt.Errorf(
        "\n\t... in WaitRunning:\n\t%w", ctx.Err(),
      )
    case <-time.After(StatePoll):
    }
  }
}
