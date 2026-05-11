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

public class CreateAssetGroup_Demo {

    private static final BitSet URLENCODER = new BitSet(256);

    private static final String CONST_ENCODE = "0123456789ABCDEF";
    public static final Charset UTF_8 = StandardCharsets.UTF_8;

    private final String region;
    private final String service;
    private final String schema;
    private final String host;
    private final String path;
    private final String ak;
    private final String sk;

    static {
        int i;
        for (i = 97; i <= 122; ++i) {
            URLENCODER.set(i);
        }

        for (i = 65; i <= 90; ++i) {
            URLENCODER.set(i);
        }

        for (i = 48; i <= 57; ++i) {
            URLENCODER.set(i);
        }
        URLENCODER.set('-');
        URLENCODER.set('_');
        URLENCODER.set('.');
        URLENCODER.set('~');
    }

    /**
     * 构造函数，初始化必要的请求参数
     */
    public CreateAssetGroup_Demo(String region, String service, String schema, String host, String path, String ak, String sk) {
        this.region = region;
        this.service = service;
        this.host = host;
        this.schema = schema;
        this.path = path;
        this.ak = ak;
        this.sk = sk;
    }

    /**
     * 主程序执行入口，用于演示创建 Asset Group 的 API 调用
     */
    public static void main(String[] args) throws Exception {
        String AccessKeyID = "AK";
        String SecretAccessKey = "SK";
        // 请求地址
        String endpoint = "ark.ap-southeast-1.byteplusapi.com";
        String path = "/"; // 路径，不包含 Query
        // 请求接口信息
        String service = "ark";
        String region = "ap-southeast-1";
        String schema = "https";
        CreateAssetGroup_Demo sign = new CreateAssetGroup_Demo(region, service, schema, endpoint, path, AccessKeyID, SecretAccessKey);

        String action = "CreateAssetGroup";
        String version = "2024-01-01";

        Date date = new Date();
        HashMap<String, String> queryMap = new HashMap<String,String>() {{
        }};

        String body = "{\"Name\": \"test-group\", \"Description\": \"test\", \"GroupType\": \"AIGC\"}";

        System.out.println("==================================================");
        System.out.println("正在发起请求: Action=" + action + ", Version=" + version);
        System.out.println("请求体: " + body);
        System.out.println("==================================================");

        sign.doRequest("POST", queryMap, body.getBytes(), date, action, version);
    }

    /**
     * 封装对 OpenAPI 的签名计算和请求调用过程
     * 
     * @param method HTTP 请求方法
     * @param queryList URL 查询参数字典
     * @param body HTTP 请求体字节流
     * @param date 请求发起的时间
     * @param action API Action 名称
     * @param version API 版本
     */
    public void doRequest(String method, Map<String, String> queryList, byte[] body,
                          Date date, String action, String version) throws Exception {
        if (body == null) {
            body = new byte[0];
        }
        String xContentSha256 = hashSHA256(body);
        SimpleDateFormat sdf = new SimpleDateFormat("yyyyMMdd'T'HHmmss'Z'");
        sdf.setTimeZone(TimeZone.getTimeZone("GMT"));
        String xDate = sdf.format(date);
        String shortXDate = xDate.substring(0, 8);
        String contentType = "application/json";

        // Header 必须按照字母序排列
        String signHeader = "content-type;host;x-content-sha256;x-date";

        SortedMap<String, String> realQueryList = new TreeMap<>(queryList);
        realQueryList.put("Action", action);
        realQueryList.put("Version", version);
        StringBuilder querySB = new StringBuilder();
        for (String key : realQueryList.keySet()) {
            querySB.append(signStringEncoder(key)).append("=").append(signStringEncoder(realQueryList.get(key))).append("&");
        }
        querySB.deleteCharAt(querySB.length() - 1);

        // CanonicalRequest 中的 headers 也必须按照字母序排列
        String canonicalStringBuilder = method + "\n" + path + "\n" + querySB + "\n" +
                "content-type:" + contentType + "\n" +
                "host:" + host + "\n" +
                "x-content-sha256:" + xContentSha256 + "\n" +
                "x-date:" + xDate + "\n" +
                "\n" +
                signHeader + "\n" +
                xContentSha256;

        String hashcanonicalString = hashSHA256(canonicalStringBuilder.getBytes());
        String credentialScope = shortXDate + "/" + region + "/" + service + "/request";
        String signString = "HMAC-SHA256" + "\n" + xDate + "\n" + credentialScope + "\n" + hashcanonicalString;

        byte[] signKey = genSigningSecretKeyV4(sk, shortXDate, region, service);
        String signature = HexFormat.of().formatHex(hmacSHA256(signKey, signString));

        URL url = new URL(schema + "://" + host + path + "?" + querySB);

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
            OutputStream os = conn.getOutputStream();
            os.write(body);
            os.flush();
            os.close();
        }
        conn.connect();

        int responseCode = conn.getResponseCode();

        InputStream is;
        if (responseCode >= 200 && responseCode < 300) {
            is = conn.getInputStream();
        } else {
            is = conn.getErrorStream();
        }
        String responseBody = new String(is.readAllBytes());
        is.close();

        System.out.println("响应状态码: " + responseCode);
        System.out.println("响应内容:\n" + responseBody);
    }

    /**
     * 规范化查询参数（URL 编码）
     * 
     * @param source 原始字符串
     * @return 编码后的字符串
     */
    private String signStringEncoder(String source) {
        if (source == null) {
            return null;
        }
        StringBuilder buf = new StringBuilder(source.length());
        ByteBuffer bb = UTF_8.encode(source);
        while (bb.hasRemaining()) {
            int b = bb.get() & 255;
            if (URLENCODER.get(b)) {
                buf.append((char) b);
            } else if (b == 32) {
                buf.append("%20");
            } else {
                buf.append("%");
                char hex1 = CONST_ENCODE.charAt(b >> 4);
                char hex2 = CONST_ENCODE.charAt(b & 15);
                buf.append(hex1);
                buf.append(hex2);
            }
        }

        return buf.toString();
    }

    /**
     * 使用 SHA256 算法计算内容的哈希值
     * 
     * @param content 待哈希的字节流
     * @return 哈希后的十六进制字符串
     */
    public static String hashSHA256(byte[] content) throws Exception {
        try {
            MessageDigest md = MessageDigest.getInstance("SHA-256");

            return HexFormat.of().formatHex(md.digest(content));
        } catch (Exception e) {
            throw new Exception(
                    "Unable to compute hash while signing request: "
                            + e.getMessage(), e);
        }
    }

    /**
     * 使用 HMAC-SHA256 算法生成签名
     * 
     * @param key 签名的密钥
     * @param content 待签名的内容
     * @return 签名后的字节流
     */
    public static byte[] hmacSHA256(byte[] key, String content) throws Exception {
        try {
            Mac mac = Mac.getInstance("HmacSHA256");
            mac.init(new SecretKeySpec(key, "HmacSHA256"));
            return mac.doFinal(content.getBytes());
        } catch (Exception e) {
            throw new Exception(
                    "Unable to calculate a request signature: "
                            + e.getMessage(), e);
        }
    }

    /**
     * 生成 V4 版本的签名密钥
     * 
     * @param secretKey 用户的 SecretAccessKey
     * @param date 日期字符串
     * @param region 区域
     * @param service 服务名称
     * @return 生成的签名密钥字节流
     */
    private byte[] genSigningSecretKeyV4(String secretKey, String date, String region, String service) throws Exception {
        byte[] kDate = hmacSHA256((secretKey).getBytes(), date);
        byte[] kRegion = hmacSHA256(kDate, region);
        byte[] kService = hmacSHA256(kRegion, service);
        return hmacSHA256(kService, "request");
    }
}

    
