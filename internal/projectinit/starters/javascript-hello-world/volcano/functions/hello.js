exports.handler = async () => {
  const greeting = process.env.GREETING || "Hello from Volcano";

  return {
    statusCode: 200,
    headers: {
      "content-type": "application/json"
    },
    body: JSON.stringify({
      message: greeting
    })
  };
};
