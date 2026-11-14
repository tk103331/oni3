// All five extension exports are optional. Names become demo__<local-name>
// when this module is configured under plugins.demo.
export const tools = {
  greet: {
    description: "Greet someone in the configured language",
    parameters: {
      type: "object",
      properties: { name: { type: "string", description: "Person to greet" } },
      required: ["name"],
    },
    execute(args, context) {
      return `${context.config.greeting ?? "Hello"}, ${args.name}!`;
    },
  },
  stats: {
    description: "Count characters and words in text",
    parameters: {
      type: "object",
      properties: { text: { type: "string" } },
      required: ["text"],
    },
    async execute({ text }) {
      await new Promise(resolve => setTimeout(resolve, 1));
      return {
        characters: Array.from(text).length,
        words: text.trim() ? text.trim().split(/\s+/).length : 0,
      };
    },
  },
};

// Models reuse a provider configured in YAML; credentials stay in that provider.
export const models = {
  fast: { provider: "local", model: "qwen3", temperature: 0.3 },
};

export const agents = {
  helper: {
    description: "Greet people and analyze text",
    system: "Use the available tools to greet people and count text. Finish the requested task.",
    model: "demo__fast",
    tools: ["demo__greet", "demo__stats"],
  },
};

export const themes = {
  ocean: {
    description: "Ocean blue",
    light: {
      primary: "#0369A1", accent: "#0E7490", success: "#047857",
      warning: "#B45309", error: "#B91C1C", user: "#4338CA", muted: "#64748B",
    },
    dark: {
      primary: "#7DD3FC", accent: "#67E8F9", success: "#6EE7B7",
      warning: "#FCD34D", error: "#FCA5A5", user: "#A5B4FC", muted: "#94A3B8",
    },
  },
};

// Hooks receive an event snapshot and the same context argument as tools.
// Throwing reports a failure; hook return values are ignored.
export const hooks = {
  before_agent(event) { console.info("Starting agent", event.agent); },
  after_agent(event) { console.info("Finished agent", event.agent, event.error ?? "ok"); },
  before_tool(event) { console.info("Calling tool", event.tool, event.call_id); },
  after_tool(event) { console.info("Finished tool", event.tool, event.error ?? "ok"); },
};
