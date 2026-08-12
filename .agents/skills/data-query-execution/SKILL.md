---
name: data-query-execution
description: Guide and enforce database data query execution via Data Query API with user access token header.
---

# Data Query Execution Skill (数据查询执行规则)

本 Skill 约束智能体在编排或分配执行数据库查询任务时的行为规范。

## 强制规则 (Mandatory Rules)

### 1. 数据值查询必须通过数据查询接口获取
- 一旦任务需要接触到具体的数据库**数据值**（例如查询具体表行、列值、业务数据等），智能体**必须**通过调用【数据查询接口】获取数据。
- 严禁绕过数据查询接口直接直连数据库读取具体数据内容。

### 2. 接口参数与 Header 配置
- **接口地址**: `$EXTERNAL_DATA_QUERY_API_URL` (缺省默认值: `http://192.168.0.138:8082/data-integrate/database/table-data/26`)
- **HTTP 方法**: `POST`
- **Headers**:
  - `Authorization`: `<access_token>` (优先从环境变量 `$DATA_QUERY_TOKEN` 或 `$AUTH_TOKEN` 中获取)
  - `Content-Type`: `application/json`
- **Request Body 限制**:
  - body request **只能**包含单条 `SELECT` SQL 查询语句。
  - 请求 JSON 结构例如：
    ```json
    {
      "sql": "SELECT * FROM `ws_poc`.`ods_poc_sales_order_header`;"
    }
    ```

### 3. DDL 与非数据值查询说明
- 数据查询接口仅支持单条 `SELECT` 查询。
- 若需进行 DDL（如建表、删表）或查看数据库结构/元数据等非数据值的操作，可使用其他标准管理工具或接口进行，但**只要涉及到具体数据内容**，必须调用上述数据查询接口。

---

## 调用示例 (Usage Examples)

### cURL 示例
```bash
# 获取环境变量中的 Token（或传入最新 access_token）
TOKEN="${DATA_QUERY_TOKEN:-$AUTH_TOKEN}"
DATA_QUERY_URL="${EXTERNAL_DATA_QUERY_API_URL:-http://192.168.0.138:8082/data-integrate/database/table-data/26}"

curl --request POST \
  --url "$DATA_QUERY_URL" \
  --header "Authorization: $TOKEN" \
  --header "Content-Type: application/json" \
  --data '{
    "sql": "SELECT * FROM `ws_poc`.`ods_poc_sales_order_header`;"
  }'
```

### Response 格式
```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "columns": {
      "order_no": "order_no",
      "area": "area",
      "order_date": "order_date"
    },
    "rows": [
      {
        "order_no": "250213-sh001",
        "area": "华东大区",
        "order_date": "2025-02-13 00:00:00"
      }
    ]
  }
}
```

### Python 示例
```python
import os
import requests

token = os.environ.get("DATA_QUERY_TOKEN") or os.environ.get("AUTH_TOKEN")
api_url = os.environ.get("EXTERNAL_DATA_QUERY_API_URL", "http://192.168.0.138:8082/data-integrate/database/table-data/26")

headers = {
    "Authorization": token,
    "Content-Type": "application/json"
}
payload = {
    "sql": "SELECT * FROM `ws_poc`.`ods_poc_sales_order_header`;"
}

response = requests.post(api_url, headers=headers, json=payload)
result = response.json()

if result.get("code") == 0:
    data = result.get("data", {})
    columns = data.get("columns", {})
    rows = data.get("rows", [])
    print(f"Queried {len(rows)} rows.")
else:
    print(f"Query failed: {result.get('msg')}")
```
