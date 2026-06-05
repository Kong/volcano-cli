const { createFunctionVolcanoClient } = require("./_shared/volcano-client");

let VolcanoAuthClass;

async function getVolcanoAuthClass() {
  if (VolcanoAuthClass) {
    return VolcanoAuthClass;
  }

  const sdk = await import("@volcano.dev/sdk");
  VolcanoAuthClass = sdk.VolcanoAuth || sdk.default;

  if (!VolcanoAuthClass) {
    throw new Error("Failed to load VolcanoAuth from @volcano.dev/sdk");
  }

  return VolcanoAuthClass;
}

exports.handler = async (event) => {
  const input = event && typeof event === "object" ? event : {};
  const auth = input.__volcano_auth;

  if (!auth || !auth.user_id || !auth.access_token) {
    return {
      statusCode: 401,
      body: JSON.stringify({ error: "Unauthorized" })
    };
  }

  const requestedLimit = Number(input.limit || 5);
  const limit = Number.isFinite(requestedLimit) ? Math.max(1, Math.min(20, Math.floor(requestedLimit))) : 5;
  let volcano;

  try {
    const VolcanoAuth = await getVolcanoAuthClass();
    volcano = createFunctionVolcanoClient(VolcanoAuth, auth);
  } catch (error) {
    return {
      statusCode: 500,
      body: JSON.stringify({ error: error?.message || "Failed to initialize Volcano client" })
    };
  }

  try {
    const { data, error } = await volcano
      .from("notes")
      .select("id,title,content,created_at")
      .order("created_at", { ascending: false })
      .limit(limit);

    if (error) {
      return {
        statusCode: 500,
        body: JSON.stringify({ error: error.message || "Failed to query notes" })
      };
    }

    return {
      statusCode: 200,
      body: JSON.stringify({
        message: "notes-summary queried successfully",
        user_id: auth.user_id,
        limit,
        notes: data || [],
        count: Array.isArray(data) ? data.length : 0,
        generated_at: new Date().toISOString()
      })
    };
  } catch (error) {
    return {
      statusCode: 500,
      body: JSON.stringify({
        error: error?.message || "Failed to query notes"
      })
    };
  }
};
