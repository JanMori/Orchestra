"""
Data Query Execution Skill
Enforces database data query execution strictly via the Data Query HTTP API
with Authorization token header, per data-query-execution SKILL specification.
"""

import json
import os
import re
import sqlite3
import time
from typing import Dict, Any, List, Optional
from config import DataQueryAPIConfig, global_config


class DataQueryExecutionSkill:
    FORBIDDEN_KEYWORDS = [
        "DROP", "DELETE", "UPDATE", "INSERT", "TRUNCATE", "ALTER",
        "CREATE", "GRANT", "REVOKE", "REPLACE", "EXEC", "EXECUTE"
    ]

    def __init__(self, config: Optional[DataQueryAPIConfig] = None):
        self.config = config or global_config.data_query
        self.mock_mode = self.config.mock_mode
        self._mock_conn: Optional[sqlite3.Connection] = None
        if self.mock_mode:
            self._init_mock_sqlite()

    def _init_mock_sqlite(self):
        """Create sample in-memory SQLite tables matching the data source."""
        self._mock_conn = sqlite3.connect(":memory:", check_same_thread=False)
        cur = self._mock_conn.cursor()
        cur.execute("""
            CREATE TABLE dim_dept (
                dept_id INT PRIMARY KEY,
                dept_name TEXT,
                region_name TEXT,
                tenant_id INT
            );
        """)
        cur.execute("""
            CREATE TABLE dwd_orders (
                order_id INT PRIMARY KEY,
                dept_id INT,
                order_amount REAL,
                order_date TEXT,
                status TEXT,
                is_deleted INT
            );
        """)
        cur.executemany("INSERT INTO dim_dept VALUES (?, ?, ?, ?)", [
            (1, "上海营销部", "华东大区", 1001),
            (2, "杭州营销部", "华东大区", 1001),
            (3, "北京营销部", "华北大区", 1001),
            (4, "广州营销部", "华南大区", 1001),
        ])
        cur.executemany("INSERT INTO dwd_orders VALUES (?, ?, ?, ?, ?, ?)", [
            (101, 1, 3200000.0, "2026-07-10", "PAID", 0),
            (102, 2, 2014000.0, "2026-07-15", "PAID", 0),
            (103, 3, 1980000.0, "2026-07-18", "PAID", 0),
            (104, 4, 3892000.0, "2026-07-20", "PAID", 0),
            (105, 1, 500000.0, "2026-07-22", "UNPAID", 0),
            (106, 2, 300000.0, "2026-06-10", "PAID", 0),
        ])
        self._mock_conn.commit()

    def audit_sql_safety(self, statement: str) -> Dict[str, Any]:
        """Verify statement is single read-only SELECT."""
        cleaned_sql = re.sub(r"--.*", "", statement)
        cleaned_sql = re.sub(r"/\*.*?\*/", "", cleaned_sql, flags=re.DOTALL).strip()
        tokens = re.findall(r"\b[A-Za-z]+\b", cleaned_sql.upper())

        for forbidden in self.FORBIDDEN_KEYWORDS:
            if forbidden in tokens:
                return {
                    "is_safe": False,
                    "reason": f"Security violation: Forbidden keyword '{forbidden}' detected."
                }

        if not cleaned_sql.upper().startswith("SELECT"):
            return {
                "is_safe": False,
                "reason": "Security violation: Only SELECT queries are permitted."
            }

        return {"is_safe": True, "reason": "Passed read-only audit."}

    def execute_sql(
        self,
        statement: str,
        database_id: Optional[int] = None,
        task_id: Optional[int] = None,
        sql_db_type: Optional[int] = None,
        dialect: Optional[int] = None,
        max_row_num: int = 1000,
        open_trans: int = 0
    ) -> Dict[str, Any]:
        """
        Execute SQL statement strictly via the Data Query HTTP API.
        """
        # Safety audit
        audit = self.audit_sql_safety(statement)
        if not audit["is_safe"]:
            return {
                "success": False,
                "statement": statement,
                "error": audit["reason"],
                "result": None
            }

        db_id = database_id if database_id is not None else self.config.database_id
        t_id = task_id if task_id is not None else self.config.task_id
        s_db_type = sql_db_type if sql_db_type is not None else self.config.sql_db_type
        dia = dialect if dialect is not None else self.config.dialect

        if self.mock_mode:
            # Mock SQLite simulation conforming to backend JobResult schema
            try:
                start_time = time.time()
                cur = self._mock_conn.cursor()
                cur.execute(statement)
                columns = [desc[0] for desc in cur.description] if cur.description else []
                rows = cur.fetchall()
                elapsed_ms = int((time.time() - start_time) * 1000)

                row_data = [dict(zip(columns, row)) for row in rows]
                return {
                    "success": True,
                    "statement": statement,
                    "error": None,
                    "result": {
                        "results": [
                            {
                                "ifQuery": True,
                                "sql": statement,
                                "time": elapsed_ms,
                                "success": True,
                                "errorMsg": None,
                                "count": len(row_data),
                                "columns": columns,
                                "rowData": row_data,
                                "page": 1,
                                "limit": max_row_num,
                                "total": len(row_data)
                            }
                        ]
                    },
                    "startTime": time.strftime("%Y-%m-%d %H:%M:%S"),
                    "endTime": time.strftime("%Y-%m-%d %H:%M:%S")
                }
            except Exception as e:
                return {
                    "success": False,
                    "statement": statement,
                    "error": str(e),
                    "result": {
                        "results": [
                            {
                                "ifQuery": True,
                                "sql": statement,
                                "success": False,
                                "errorMsg": str(e),
                                "count": 0,
                                "columns": [],
                                "rowData": []
                            }
                        ]
                    }
                }
        else:
            # Live HTTP call per data-query-execution SKILL.md
            token = self.config.token
            if not token:
                raise ValueError("⚠️ 未检测到有效的数据查询权限凭据（DATA_QUERY_TOKEN），请确认您已通过系统账号密码登录。")

            payload = {
                "statement": statement,
                "sqlDbType": s_db_type,
                "databaseId": db_id if s_db_type == 1 else None,
                "dialect": dia,
                "id": t_id,
                "openTrans": open_trans,
                "maxRowNum": max_row_num,
                "processEnd": True,
                "type": None
            }

            headers = {
                "Authorization": token,
                "Content-Type": "application/json"
            }

            import urllib.request
            req = urllib.request.Request(
                self.config.api_url,
                data=json.dumps(payload).encode("utf-8"),
                headers=headers,
                method="POST"
            )

            try:
                with urllib.request.urlopen(req, timeout=15) as resp:
                    resp_data = json.loads(resp.read().decode("utf-8"))
                    return resp_data
            except urllib.error.HTTPError as e:
                if e.code in (401, 403):
                    raise RuntimeError("⚠️ 数据查询权限凭据已过期或失效，请在平台退出并重新登录以刷新数据权限。")
                raise RuntimeError(f"HTTP request failed: {e.code} - {e.reason}")
            except Exception as e:
                raise RuntimeError(f"Data Query API call failed: {str(e)}")
