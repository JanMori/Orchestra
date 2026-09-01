---
name: orchestra-data-query
description: Guide and enforce database data query execution via Data Query API with user access token header.
---

# Data Query Execution Skill (数据查询执行规则)

本 Skill 约束智能体在编排或分配执行数据库查询任务时的行为规范。

## 强制规则 (Mandatory Rules)

### 1. 数据值查询必须通过数据查询接口获取
- 一旦任务需要接触到具体的数据库**数据值**（例如查询具体表行、列值、业务数据等），智能体**必须**通过调用【数据查询接口】获取数据。
- 严禁绕过数据查询接口直接直连数据库读取具体数据内容。

### 2. 接口参数与 Header 配置
- **接口地址**: 必须从环境变量 `$EXTERNAL_DATA_QUERY_API_URL` 获取（格式例如: `http://<HOST>:<PORT>/data-development/task/execute-sql`）
- **HTTP 方法**: `POST`
- **Headers**:
  - `Authorization`: `<access_token>` (在执行查询前优先执行 `orchestra auth user-token` 动态获取并赋给局部变量 `$DATA_QUERY_TOKEN` / `$AUTH_TOKEN`，若未获取到则回退读取环境变量)
  - `Content-Type`: `application/json`
- **Request Body 参数说明与限制**:
  - body request **只能**包含单条 `SELECT` SQL 查询语句（填入 `statement` 字段）。
  - 参数字段说明：
    - `statement` (String, 必填): 要执行的 SQL 查询语句。
    - `sqlDbType` (Integer, 必填): 数据库类型：`1`=普通数据库，`2`=中台库。
    - `databaseId` (Integer, 必填): 目标数据源 ID（用于查 JDBC 连接信息；若 `sqlDbType` 为 2 中台库则为 `null`；优先从 `$DATA_QUERY_DATABASE_ID` 获取，缺省为 `20`）。
    - `dialect` (Integer, 必填): SQL 方言：`1`=普通 SQL（MySQL/Oracle 等），非 Flink。
    - `id` (Integer, 必填): 任务 ID（后端转成 taskId，用于查任务和存历史；优先从 `$DATA_QUERY_TASK_ID` 获取，缺省为 `69`）。
    - `openTrans` (Integer, 选填): 是否开启事务：`0`=否，`1`=是，默认 `0`。
    - `maxRowNum` (Integer, 必填): 最大返回行数，默认 `1000`。
    - `processEnd` (Boolean, 选填): 是否添加进程结束标识，默认 `true`。
    - `type` (String, 选填): 运行模式，传 `null`。
  - 请求 JSON 结构示例：
    ```json
    {
      "statement": "SELECT * from data_database",
      "sqlDbType": 1,
      "databaseId": 20,
      "dialect": 1,
      "id": 69,
      "openTrans": 0,
      "maxRowNum": 1000,
      "processEnd": true,
      "type": null
    }
    ```

### 3. DDL 与非数据值查询说明
- 数据查询接口仅支持单条 `SELECT` 查询。
- 若需进行 DDL（如建表、删表）或查看数据库结构/元数据等非数据值的操作，可使用其他标准管理工具或接口进行，但**只要涉及到具体数据内容**，必须调用上述数据查询接口。

### 4. 异常与鉴权失败处理规范 (Error Handling & Auth Recovery)
- **实时执行原则 (Live Execution First)**：
  - 智能体收到数据查询请求时，**必须直接在当前运行时通过 curl / Python 实际调用一次数据查询接口**检验结果，**严禁**未经尝试就直接引用历史 Issue、评论或对话中的错误记录断言没有 Token。
- **Token / API URL 缺失规则**：
  - 若当前运行时环境变量 `$EXTERNAL_DATA_QUERY_API_URL` 为空或未设置，智能体应向用户反馈：“⚠️ 未检测到数据查询接口地址（EXTERNAL_DATA_QUERY_API_URL），请联系管理员配置接口地址。”
  - 若动态获取及环境变量中的 `$DATA_QUERY_TOKEN` 和 `$AUTH_TOKEN` 均为空或未设置，智能体**严禁**自行猜测或发起空 Authorization 请求。
  - 智能体应直接向用户明确反馈：“⚠️ 未检测到有效的数据查询权限凭据（DATA_QUERY_TOKEN），请确认您已通过系统账号密码登录。”
- **Token 过期或鉴权失败（HTTP 401 / 403 或鉴权错误）**：
  - 若数据查询接口实际返回 HTTP `401 Unauthorized`、`403 Forbidden` 或响应体中提示鉴权失败/Token 无效，智能体**严禁**反复盲目重试。
  - 智能体应直接向用户明确反馈：“⚠️ 数据查询权限凭据已过期或失效，请在平台退出并重新登录以刷新数据权限。”

---

## 调用示例 (Usage Examples)

### cURL 示例
```bash
# 1. 动态实时获取当前用户 Token 并写入局部变量（回退读取环境变量）
DATA_QUERY_TOKEN=$(orchestra auth user-token 2>/dev/null || echo "$DATA_QUERY_TOKEN")
TOKEN="${DATA_QUERY_TOKEN:-$AUTH_TOKEN}"
DATA_QUERY_URL="${EXTERNAL_DATA_QUERY_API_URL:?未配置 EXTERNAL_DATA_QUERY_API_URL 环境变量}"
DATABASE_ID="${DATA_QUERY_DATABASE_ID:-20}"
TASK_ID="${DATA_QUERY_TASK_ID:-69}"

# 2. 携带最新 Token 发起查询
curl --request POST \
  --url "$DATA_QUERY_URL" \
  --header "Authorization: $TOKEN" \
  --header "Content-Type: application/json" \
  --data "{
    \"statement\": \"SELECT * from data_database\",
    \"sqlDbType\": 1,
    \"databaseId\": $DATABASE_ID,
    \"dialect\": 1,
    \"id\": $TASK_ID,
    \"openTrans\": 0,
    \"maxRowNum\": 1000,
    \"processEnd\": true,
    \"type\": null
  }"
```

### Response 格式 (JobResult)
```json
{
  "success": true,
  "statement": "SELECT * from data_database",
  "error": null,
  "result": {
    "results": [
      {
        "ifQuery": true,
        "sql": "SELECT * from data_database",
        "time": 45,
        "success": true,
        "errorMsg": null,
        "count": 10,
        "columns": [
          "id",
          "name",
          "database_type",
          "database_ip",
          "database_port",
          "database_name"
        ],
        "rowData": [
          {
            "id": 5,
            "name": "人口测试数据",
            "database_type": 1,
            "database_ip": "127.0.0.1",
            "database_port": "3306",
            "database_name": "srt_cloud_test"
          }
        ],
        "page": 1,
        "limit": 1000,
        "total": 10
      }
    ]
  },
  "startTime": "2026-08-20 14:15:21",
  "endTime": "2026-08-20 14:15:22"
}
```

### Python 示例
```python
import os
import subprocess
import requests

def get_data_query_token():
    # 优先执行 CLI 实时从后端获取当前用户 Token
    try:
        res = subprocess.run(["orchestra", "auth", "user-token"], capture_output=True, text=True)
        if res.returncode == 0 and res.stdout.strip():
            return res.stdout.strip()
    except Exception:
        pass
    # 兜底回退读取环境变量
    return os.environ.get("DATA_QUERY_TOKEN") or os.environ.get("AUTH_TOKEN")

token = get_data_query_token()
api_url = os.environ.get("EXTERNAL_DATA_QUERY_API_URL")
if not api_url:
    raise ValueError("⚠️ 未检测到数据查询接口地址（EXTERNAL_DATA_QUERY_API_URL），请配置该环境变量。")

database_id = int(os.environ.get("DATA_QUERY_DATABASE_ID", 20))
task_id = int(os.environ.get("DATA_QUERY_TASK_ID", 69))

headers = {
    "Authorization": token,
    "Content-Type": "application/json"
}

payload = {
    "statement": "SELECT * from data_database",
    "sqlDbType": 1,
    "databaseId": database_id,
    "dialect": 1,
    "id": task_id,
    "openTrans": 0,
    "maxRowNum": 1000,
    "processEnd": True,
    "type": None
}

response = requests.post(api_url, headers=headers, json=payload)
result = response.json()

if result.get("success"):
    exec_results = result.get("result", {}).get("results", [])
    if exec_results and exec_results[0].get("success"):
        query_result = exec_results[0]
        columns = query_result.get("columns", [])
        rows = query_result.get("rowData", [])
        print(f"Query succeeded. Total: {len(rows)} rows.")
    else:
        error_msg = exec_results[0].get("errorMsg") if exec_results else result.get("error")
        print(f"SQL execution failed: {error_msg}")
else:
    print(f"Request failed: {result.get('error') or result.get('msg')}")
```
