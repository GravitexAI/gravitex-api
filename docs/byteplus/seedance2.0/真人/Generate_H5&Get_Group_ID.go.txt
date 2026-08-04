package main

import (
    "errors"
    "fmt"
    "time"

    "github.com/bytedance/sonic"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/credentials"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/session"
    "github.com/byteplus-sdk/byteplus-go-sdk-v2/byteplus/universal"
)

const (
    region      = "ap-southeast-1"
    serviceName = "ark"
    version     = "2024-01-01"

    projectName = "default"
    callbackURL = "https://www.example.com/callback"

    initialWait  = 3 * time.Minute
    pollInterval = 5 * time.Second
    pollTimeout  = 10 * time.Minute
)

func main() {
    // Replace with your real AK/SK, or preferably load from env vars.
    ak := "<YOUR_AK>"
    sk := "<YOUR_SK>"

    config := byteplus.NewConfig().
        WithCredentials(credentials.NewStaticCredentials(ak, sk, "")).
        WithRegion(region)

    sess, err := session.NewSession(config)
    if err != nil {
        fmt.Printf("create session failed: %v\n", err)
        return
    }

    client := universal.New(sess)

    // Step 1: Create validation session
    bytedToken, h5Link, err := createVisualValidateSession(client, callbackURL, projectName)
    if err != nil {
        fmt.Printf("CreateVisualValidateSession failed: %v\n", err)
        return
    }

    fmt.Println("CreateVisualValidateSession succeeded")
    fmt.Printf("Validation Link: %s\n", h5Link)
    fmt.Printf("BytedToken: %s\n", bytedToken)

    // Step 2: Wait 3 minutes before polling
    fmt.Printf("Waiting %v before polling validation result...\n", initialWait)
    time.Sleep(initialWait)

    // Step 3: Poll GetVisualValidateResult
    groupID, err := waitForVisualValidateResult(client, bytedToken, projectName, pollInterval, pollTimeout)
    if err != nil {
        fmt.Printf("GetVisualValidateResult failed: %v\n", err)
        return
    }

    fmt.Printf("Validation succeeded, GroupId = %s\n", groupID)
}

func createVisualValidateSession(client *universal.Universal, callbackURL, projectName string) (bytedToken, h5Link string, err error) {
    resp, err := client.DoCall(
        universal.RequestUniversal{
            ServiceName: serviceName,
            Action:      "CreateVisualValidateSession",
            Version:     version,
            HttpMethod:  universal.POST,
            ContentType: universal.ApplicationJSON,
        },
        &map[string]any{
            "CallbackURL": callbackURL,
            "ProjectName": projectName,
        },
    )
    if err != nil {
        return "", "", err
    }
    if resp == nil {
        return "", "", errors.New("CreateVisualValidateSession response is nil")
    }

    respData, _ := sonic.Marshal(resp)
    fmt.Printf("CreateVisualValidateSession response: %s\n", string(respData))

    bytedToken = extractString(resp, "BytedToken")
    if bytedToken == "" {
        bytedToken = extractString(resp, "Result", "BytedToken")
    }

    h5Link = extractString(resp, "H5Link")
    if h5Link == "" {
        h5Link = extractString(resp, "Result", "H5Link")
    }

    if bytedToken == "" {
        return "", "", fmt.Errorf("cannot find BytedToken in response: %s", string(respData))
    }

    return bytedToken, h5Link, nil
}

func waitForVisualValidateResult(client *universal.Universal, bytedToken, projectName string, interval, timeout time.Duration) (string, error) {
    deadline := time.Now().Add(timeout)

    for {
        if time.Now().After(deadline) {
            return "", fmt.Errorf("polling timeout after %v, BytedToken=%s", timeout, bytedToken)
        }

        groupID, apiErr, rawResp, err := getVisualValidateResult(client, bytedToken, projectName)
        if err != nil {
            return "", err
        }

        fmt.Printf("GetVisualValidateResult response: %s\n", rawResp)

        if groupID != "" {
            return groupID, nil
        }

        if apiErr != "" {
            return "", fmt.Errorf("validation returned error: %s", apiErr)
        }

        fmt.Printf("GroupId not ready yet, retrying in %v...\n", interval)
        time.Sleep(interval)
    }
}

func getVisualValidateResult(client *universal.Universal, bytedToken, projectName string) (groupID, apiErr, rawResp string, err error) {
    resp, err := client.DoCall(
        universal.RequestUniversal{
            ServiceName: serviceName,
            Action:      "GetVisualValidateResult",
            Version:     version,
            HttpMethod:  universal.POST,
            ContentType: universal.ApplicationJSON,
        },
        &map[string]any{
            "BytedToken":  bytedToken,
            "ProjectName": projectName,
        },
    )
    if err != nil {
        return "", "", "", err
    }
    if resp == nil {
        return "", "", "", errors.New("GetVisualValidateResult response is nil")
    }

    respData, _ := sonic.Marshal(resp)
    rawResp = string(respData)

    groupID = extractString(resp, "GroupId")
    if groupID == "" {
        groupID = extractString(resp, "Result", "GroupId")
    }

    // Try several common error locations
    apiErr = extractString(resp, "Error")
    if apiErr == "" {
        apiErr = extractString(resp, "Result", "Error")
    }
    if apiErr == "" {
        apiErr = extractString(resp, "ResponseMetadata", "Error", "Message")
    }

    return groupID, apiErr, rawResp, nil
}

func extractString(data any, keys ...string) string {
    current := data

    for _, key := range keys {
        switch v := current.(type) {
        case map[string]any:
            next, ok := v[key]
            if !ok {
                return ""
            }
            current = next

        case *map[string]any:
            if v == nil {
                return ""
            }
            next, ok := (*v)[key]
            if !ok {
                return ""
            }
            current = next

        default:
            return ""
        }
    }

    switch v := current.(type) {
    case string:
        return v
    case fmt.Stringer:
        return v.String()
    case nil:
        return ""
    default:
        return fmt.Sprintf("%v", v)
    }
}
