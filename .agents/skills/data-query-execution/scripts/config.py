# Unified Auto-resolving config for Multi-Agent Skills
import os
from dataclasses import dataclass, field

def _load_dotenv():
    current_script_dir = os.path.dirname(os.path.abspath(__file__))
    parent_skill_dir = os.path.dirname(current_script_dir)
    search_paths = [
        os.path.join(parent_skill_dir, ".env"),
        os.path.join(current_script_dir, ".env"),
        os.path.join(os.getcwd(), ".env"),
        os.path.join(os.path.dirname(parent_skill_dir), ".env")
    ]
    for env_path in search_paths:
        if os.path.exists(env_path):
            with open(env_path, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if line and not line.startswith("#") and "=" in line:
                        k, v = line.split("=", 1)
                        k, v = k.strip(), v.strip().strip("'\"")
                        if k not in os.environ:
                            os.environ[k] = v
            break

_load_dotenv()

@dataclass
class MilvusConfig:
    host: str = field(default_factory=lambda: os.getenv("MILVUS_HOST", "127.0.0.1"))
    port: int = field(default_factory=lambda: int(os.getenv("MILVUS_PORT", "19530")))
    user: str = field(default_factory=lambda: os.getenv("MILVUS_USER", ""))
    password: str = field(default_factory=lambda: os.getenv("MILVUS_PASSWORD", ""))
    db_name: str = field(default_factory=lambda: os.getenv("MILVUS_DB", "default"))
    experience_collection: str = field(default_factory=lambda: os.getenv("MILVUS_EXP_COLLECTION", "experience_cases"))
    ontology_collection: str = field(default_factory=lambda: os.getenv("MILVUS_ONTO_COLLECTION", "ontology_entities"))
    dim: int = field(default_factory=lambda: int(os.getenv("MILVUS_DIM", "1024")))
    mock_mode: bool = field(default_factory=lambda: os.getenv("MILVUS_MOCK_MODE", "true").lower() == "true")

@dataclass
class EmbeddingConfig:
    provider: str = field(default_factory=lambda: os.getenv("EMBEDDING_PROVIDER", "mock"))
    model_name: str = field(default_factory=lambda: os.getenv("EMBEDDING_MODEL", "BAAI/bge-large-zh-v1.5"))
    api_url: str = field(default_factory=lambda: os.getenv("EMBEDDING_API_URL", "https://api.siliconflow.cn/v1/embeddings"))
    api_key: str = field(default_factory=lambda: os.getenv("EMBEDDING_API_KEY", ""))
    mock_mode: bool = field(default_factory=lambda: os.getenv("EMBEDDING_MOCK_MODE", "true").lower() == "true")

@dataclass
class HugeGraphConfig:
    host: str = field(default_factory=lambda: os.getenv("HUGEGRAPH_HOST", "127.0.0.1"))
    port: int = field(default_factory=lambda: int(os.getenv("HUGEGRAPH_PORT", "8080")))
    graph_name: str = field(default_factory=lambda: os.getenv("HUGEGRAPH_GRAPH", "hugegraph"))
    username: str = field(default_factory=lambda: os.getenv("HUGEGRAPH_USERNAME", "admin"))
    password: str = field(default_factory=lambda: os.getenv("HUGEGRAPH_PASSWORD", "admin"))
    timeout_sec: int = field(default_factory=lambda: int(os.getenv("HUGEGRAPH_TIMEOUT", "10")))
    mock_mode: bool = field(default_factory=lambda: os.getenv("HUGEGRAPH_MOCK_MODE", "true").lower() == "true")

    @property
    def base_url(self) -> str:
        return f"http://{self.host}:{self.port}"

import subprocess

def _resolve_data_query_token() -> str:
    try:
        res = subprocess.run(["orchestra", "auth", "user-token"], capture_output=True, text=True, timeout=5)
        if res.returncode == 0 and res.stdout.strip():
            return res.stdout.strip()
    except Exception:
        pass
    return os.getenv("DATA_QUERY_TOKEN", os.getenv("AUTH_TOKEN", ""))

@dataclass
class DataQueryAPIConfig:
    api_url: str = field(default_factory=lambda: os.getenv("EXTERNAL_DATA_QUERY_API_URL", ""))
    token: str = field(default_factory=_resolve_data_query_token)
    database_id: int = field(default_factory=lambda: int(os.getenv("DATA_QUERY_DATABASE_ID", "20")))
    task_id: int = field(default_factory=lambda: int(os.getenv("DATA_QUERY_TASK_ID", "69")))
    sql_db_type: int = field(default_factory=lambda: int(os.getenv("DATA_QUERY_SQL_DB_TYPE", "1")))
    dialect: int = field(default_factory=lambda: int(os.getenv("DATA_QUERY_DIALECT", "1")))
    max_row_num: int = field(default_factory=lambda: int(os.getenv("DATA_QUERY_MAX_ROW_NUM", "1000")))
    mock_mode: bool = field(default_factory=lambda: os.getenv("DATA_QUERY_MOCK_MODE", "true").lower() == "true")

@dataclass
class SystemConfig:
    milvus: MilvusConfig = field(default_factory=MilvusConfig)
    embedding: EmbeddingConfig = field(default_factory=EmbeddingConfig)
    hugegraph: HugeGraphConfig = field(default_factory=HugeGraphConfig)
    data_query: DataQueryAPIConfig = field(default_factory=DataQueryAPIConfig)

global_config = SystemConfig()
