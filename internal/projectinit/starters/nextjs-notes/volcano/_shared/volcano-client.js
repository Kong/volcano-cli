const {
  VOLCANO_API_URL,
  VOLCANO_ANON_KEY,
  VOLCANO_DATABASE,
} = process.env;

function createVolcanoClient(VolcanoAuthClass, { apiUrl, anonKey, database = "app", accessToken }) {
  if (!apiUrl || !anonKey) {
    throw new Error("Missing VOLCANO_API_URL or VOLCANO_ANON_KEY");
  }
  if (!VolcanoAuthClass) {
    throw new Error("Missing VolcanoAuth constructor");
  }

  const volcano = new VolcanoAuthClass({
    apiUrl,
    anonKey,
    ...(accessToken && { accessToken }),
  });

  return volcano.database(database);
}

const createWebVolcanoClient = (VolcanoAuthClass) =>
  createVolcanoClient(VolcanoAuthClass, {
    apiUrl: process.env.NEXT_PUBLIC_VOLCANO_API_URL || VOLCANO_API_URL,
    anonKey: process.env.NEXT_PUBLIC_VOLCANO_ANON_KEY || VOLCANO_ANON_KEY,
    database: process.env.NEXT_PUBLIC_VOLCANO_DATABASE || VOLCANO_DATABASE || "app",
  });

const createFunctionVolcanoClient = (VolcanoAuthClass, auth) => {
  if (!auth?.access_token) {
    throw new Error("Missing function auth access token");
  }

  return createVolcanoClient(VolcanoAuthClass, {
    apiUrl: VOLCANO_API_URL,
    anonKey: VOLCANO_ANON_KEY,
    database: VOLCANO_DATABASE || "app",
    accessToken: auth.access_token,
  });
};

module.exports = {
  createVolcanoClient,
  createWebVolcanoClient,
  createFunctionVolcanoClient,
};
