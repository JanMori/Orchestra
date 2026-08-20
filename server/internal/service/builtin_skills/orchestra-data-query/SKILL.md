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

### 4. 异常与鉴权失败处理规范 (Error Handling & Auth Recovery)
- **实时执行原则 (Live Execution First)**：
  - 智能体收到数据查询请求时，**必须直接在当前运行时通过 curl / Python 实际调用一次数据查询接口**检验结果，**严禁**未经尝试就直接引用历史 Issue、评论或对话中的错误记录断言没有 Token。
- **Token 缺失规则**：
  - 若在当前运行时环境变量 `$DATA_QUERY_TOKEN` 和 `$AUTH_TOKEN` 均为空或未设置，智能体**严禁**自行猜测或发起空 Authorization 请求。
  - 智能体应直接向用户明确反馈：“⚠️ 未检测到有效的数据查询权限凭据（DATA_QUERY_TOKEN），请确认您已通过系统账号密码登录。”
- **Token 过期或鉴权失败（HTTP 401 / 403 或鉴权错误）**：
  - 若数据查询接口实际返回 HTTP `401 Unauthorized`、`403 Forbidden` 或响应体中提示鉴权失败/Token 无效，智能体**严禁**反复盲目重试。
  - 智能体应直接向用户明确反馈：“⚠️ 数据查询权限凭据已过期或失效，请在平台退出并重新登录以刷新数据权限。”

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
