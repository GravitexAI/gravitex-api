      
package com.ark.sample;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.ByteBuffer;
import java.nio.charset.Charset;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.text.SimpleDateFormat;
import java.util.*;
import org.json.JSONObject;

/**
 * Copyright (year) Beijing Volcano Engine Technology Ltd.
 * <p>
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 * <p>
 * http://www.apache.org/licenses/LICENSE-2.0
 * <p>
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

public class CreateAsset_Demo {

    private static final BitSet URLENCODER = new BitSet(256);
    private static final String CONST_ENCODE = "0123456789ABCDEF";
    public static final Charset UTF_8 = StandardCharsets.UTF_8;

    // 轮询配置
    private static final long POLL_INTERVAL_MS = 3000;   // 3 秒
    private static final long POLL_TIMEOUT_MS = 120000; // 2 分钟

    private final String region;
    private final String service;
    private final String schema;
    private final String host;
    private final String path;
    private final String ak;
    private final String sk;

    static {
        int i;
        for (i = 97; i <= 122; ++i) URLENCODER.set(i);
        for (i = 65; i <= 90; ++i) URLENCODER.set(i);
        for (i = 48; i <= 57; ++i) URLENCODER.set(i);
        URLENCODER.set('-');
        URLENCODER.set('_');
        URLENCODER.set('.');
        URLENCODER.set('~');
    }

    /**
     * 构造函数，初始化必要的请求参数
     */
    public CreateAsset_Demo(String region, String service, String schema, String host, String path, String ak, String sk) {
        this.region = region;
        this.service = service;
        this.host = host;
        this.schema = schema;
        this.path = path;
        this.ak = ak;
        this.sk = sk;
    }

    public static void main(String[] args) throws Exception {
        // TODO: 替换为真实的 AK/SK
        String AccessKeyID = "AK";
        String SecretAccessKey = "SK";

        String endpoint = "ark.ap-southeast-1.byteplusapi.com";
        String path = "/";
        String service = "ark";
        String region = "ap-southeast-1";
        String schema = "https";
        CreateAsset_Demo sign = new CreateAsset_Demo(region, service, schema, endpoint, path, AccessKeyID, SecretAccessKey);

        // 1. 创建资产
        String createAction = "CreateAsset";
        String version = "2024-01-01";
        String createBody = "{\"GroupId\": \"group-20260329162846-*****\", \"URL\": \"https://ark-doc.tos-ap-southeast-1.bytepluses.com/doc_image/r2v_tea_pic1.jpg\", \"AssetType\": \"Image\", \"ProjectName\": \"default\"}";

        System.out.println("==================================================");
        System.out.println("正在发起 CreateAsset 请求...");
        System.out.println("请求体: " + createBody);
        System.out.println("==================================================");

        String createResponse = sign.callAPI("POST", new HashMap<>(), createBody.getBytes(), new Date(), createAction, version);
        System.out.println("CreateAsset 响应: " + createResponse);

        // 解析 AssetId
        JSONObject createRespJson = new JSONObject(createResponse);
        String assetId = extractAssetId(createRespJson);
        if (assetId == null || assetId.isEmpty()) {
            System.out.println("无法从响应中提取 AssetId");
            return;
        }
        System.out.println("Asset created, AssetId = " + assetId);

        // 2. 轮询资产状态直到 Active
        boolean success = sign.waitForAssetActive(assetId);
        if (success) {
            System.out.println("Asset is active.");
        } else {
            System.out.println("Asset polling failed.");
        }
    }

    /**
     * 从 CreateAsset 响应中提取 AssetId，兼容不同返回结构
     */
    private static String extractAssetId(JSONObject resp) {
        if (resp.has("Result")) {
            JSONObject result = resp.getJSONObject("Result");
            if (result.has("Id")) return result.getString("Id");
            if (result.has("AssetId")) return result.getString("AssetId");
        }
        if (resp.has("Id")) return resp.getString("Id");
        if (resp.has("AssetId")) return resp.getString("AssetId");
        return null;
    }

    /**
     * 轮询 GetAsset 直到资产变为 Active 或超时/失败
     */
    private boolean waitForAssetActive(String assetId) throws Exception {
        long start = System.currentTimeMillis();
        while (System.currentTimeMillis() - start < POLL_TIMEOUT_MS) {
            AssetStatus status = getAssetStatus(assetId);
            System.out.println("Asset status: " + status.status);
            System.out.println("GetAsset response:");
            System.out.println(status.rawResponse);

            if ("Active".equalsIgnoreCase(status.status)) {
                return true;
            } else if ("Failed".equalsIgnoreCase(status.status)) {
                String errMsg = status.errorMsg != null ? status.errorMsg : "unknown error";
                System.out.println("Asset processing failed: " + errMsg);
                return false;
            } else if ("Processing".equalsIgnoreCase(status.status)) {
                Thread.sleep(POLL_INTERVAL_MS);
                continue;
            } else {
                System.out.println("Unexpected status '" + status.status + "', continue polling...");
                Thread.sleep(POLL_INTERVAL_MS);
            }
        }
        System.out.println("Polling timeout after " + POLL_TIMEOUT_MS + " ms, assetId=" + assetId);
        return false;
    }

    /**
     * 调用 GetAsset 并返回状态信息
     */
    private AssetStatus getAssetStatus(String assetId) throws Exception {
        String getAction = "GetAsset";
        String version = "2024-01-01";
        String body = "{\"Id\": \"" + assetId + "\"}";
        String response = callAPI("POST", new HashMap<>(), body.getBytes(), new Date(), getAction, version);

        JSONObject respJson = new JSONObject(response);
        String status = extractField(respJson, "Result", "Status");
        if (status == null) status = extractField(respJson, "Status");

        String url = extractField(respJson, "Result", "URL");
        if (url == null) url = extractField(respJson, "URL");

        String errorMsg = extractField(respJson, "Result", "Error");
        if (errorMsg == null) errorMsg = extractField(respJson, "Error");

        return new AssetStatus(status, url, errorMsg, response);
    }

    /**
     * 从 JSONObject 中安全提取嵌套字段的值
     */
    private String extractField(JSONObject obj, String... keys) {
        Object current = obj;
        for (String key : keys) {
            if (current instanceof JSONObject) {
                current = ((JSONObject) current).opt(key);
                if (current == null) return null;
            } else {
                return null;
            }
        }
        return current != null ? current.toString() : null;
    }

    /**
     * 执行 API 调用并返回响应体字符串
     */
    private String callAPI(String method, Map<String, String> queryMap, byte[] body,
                           Date date, String action, String version) throws Exception {
        if (body == null) body = new byte[0];
        String xContentSha256 = hashSHA256(body);
        SimpleDateFormat sdf = new SimpleDateFormat("yyyyMMdd'T'HHmmss'Z'");
        sdf.setTimeZone(TimeZone.getTimeZone("GMT"));
        String xDate = sdf.format(date);
        String shortXDate = xDate.substring(0, 8);
        String contentType = "application/json";
        String signHeader = "content-type;host;x-content-sha256;x-date";

        SortedMap<String, String> realQueryMap = new TreeMap<>(queryMap);
        realQueryMap.put("Action", action);
        realQueryMap.put("Version", version);
        StringBuilder querySB = new StringBuilder();
        for (String key : realQueryMap.keySet()) {
            querySB.append(signStringEncoder(key)).append("=").append(signStringEncoder(realQueryMap.get(key))).append("&");
        }
        if (querySB.length() > 0) querySB.deleteCharAt(querySB.length() - 1);

        String canonicalRequest = method + "\n" + this.path + "\n" + querySB + "\n" +
                "content-type:" + contentType + "\n" +
                "host:" + host + "\n" +
                "x-content-sha256:" + xContentSha256 + "\n" +
                "x-date:" + xDate + "\n" +
                "\n" +
                signHeader + "\n" +
                xContentSha256;

        String hashedCanonicalRequest = hashSHA256(canonicalRequest.getBytes());
        String credentialScope = shortXDate + "/" + region + "/" + service + "/request";
        String stringToSign = "HMAC-SHA256" + "\n" + xDate + "\n" + credentialScope + "\n" + hashedCanonicalRequest;

        byte[] signingKey = genSigningSecretKeyV4(sk, shortXDate, region, service);
        String signature = HexFormat.of().formatHex(hmacSHA256(signingKey, stringToSign));

        URL url = new URL(schema + "://" + host + this.path + "?" + querySB);
        HttpURLConnection conn = (HttpURLConnection) url.openConnection();
        conn.setRequestMethod(method);
        conn.setRequestProperty("Host", host);
        conn.setRequestProperty("X-Date", xDate);
        conn.setRequestProperty("X-Content-Sha256", xContentSha256);
        conn.setRequestProperty("Content-Type", contentType);
        conn.setRequestProperty("Authorization", "HMAC-SHA256" +
                " Credential=" + ak + "/" + credentialScope +
                ", SignedHeaders=" + signHeader +
                ", Signature=" + signature);
        if (!Objects.equals(conn.getRequestMethod(), "GET")) {
            conn.setDoOutput(true);
            try (OutputStream os = conn.getOutputStream()) {
                os.write(body);
                os.flush();
            }
        }
        conn.connect();

        int responseCode = conn.getResponseCode();
        InputStream is = (responseCode >= 200 && responseCode < 300) ? conn.getInputStream() : conn.getErrorStream();
        String responseBody = new String(is.readAllBytes());
        is.close();

        if (responseCode != 200) {
            throw new Exception("HTTP " + responseCode + ": " + responseBody);
        }
        return responseBody;
    }

    // ------------------ 签名辅助方法（与原有逻辑一致）------------------
    private String signStringEncoder(String source) {
        if (source == null) return null;
        StringBuilder buf = new StringBuilder(source.length());
        ByteBuffer bb = UTF_8.encode(source);
        while (bb.hasRemaining()) {
            int b = bb.get() & 0xFF;
            if (URLENCODER.get(b)) {
                buf.append((char) b);
            } else if (b == 32) {
                buf.append("%20");
            } else {
                buf.append('%');
                buf.append(CONST_ENCODE.charAt(b >> 4));
                buf.append(CONST_ENCODE.charAt(b & 0xF));
            }
        }
        return buf.toString();
    }

    public static String hashSHA256(byte[] content) throws Exception {
        try {
            MessageDigest md = MessageDigest.getInstance("SHA-256");
            return HexFormat.of().formatHex(md.digest(content));
        } catch (Exception e) {
            throw new Exception("Unable to compute hash while signing request: " + e.getMessage(), e);
        }
    }

    public static byte[] hmacSHA256(byte[] key, String content) throws Exception {
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(key, "HmacSHA256"));
            return mac.doFinal(content.getBytes(UTF_8));
        } catch (Exception e) {
            throw new Exception("Unable to calculate a request signature: " + e.getMessage(), e);
        }
    }

    private byte[] genSigningSecretKeyV4(String secretKey, String date, String region, String service) throws Exception {
        byte[] kDate = hmacSHA256(secretKey.getBytes(UTF_8), date);
        byte[] kRegion = hmacSHA256(kDate, region);
        byte[] kService = hmacSHA256(kRegion, service);
        return hmacSHA256(kService, "request");
    }

    /**
     * 内部类，封装 GetAsset 返回的状态信息
     */
    private static class AssetStatus {
        String status;
        String url;
        String errorMsg;
        String rawResponse;

        AssetStatus(String status, String url, String errorMsg, String rawResponse) {
            this.status = status;
            this.url = url;
            this.errorMsg = errorMsg;
            this.rawResponse = rawResponse;
        }
    }
}

    
