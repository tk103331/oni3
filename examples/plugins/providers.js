// Provider definitions are discovered without making a network request.
// /provider asks for a key when one is not configured, then fetches the catalog.
export const providers = {
  cloud: {
    description: "OpenAI-compatible API",
    type: "openai",
    base_url: "https://api.openai.com/v1",
    models_url: "models",          // relative to base_url, retaining /v1
    models_path: "data",           // array in the response
    model_id_path: "id",           // model ID within each array item
  },
  local: {
    description: "Local Ollama",
    type: "ollama",
    base_url: "http://localhost:11434",
    models_url: "/api/tags",       // path from the origin root
    models_path: "models",
    model_id_path: "name",
    api_key_optional: true,
  },
};
