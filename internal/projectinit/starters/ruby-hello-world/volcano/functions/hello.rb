require "json"

def handler(event:, context:)
  {
    statusCode: 200,
    headers: { "Content-Type" => "application/json" },
    body: JSON.generate({
      message: "Hello from Volcano Ruby!",
      event: event
    })
  }
end
