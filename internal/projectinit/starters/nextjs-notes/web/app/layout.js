import "./globals.css";

export const metadata = {
  title: "Volcano Next.js Demo",
  description: "Minimal Volcano Next.js notes demo"
};

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <body>
        <main className="container">
          {children}
          <footer className="site-footer">
            Powered by{" "}
            <a href="https://volcano.dev" target="_blank" rel="noreferrer">
              Volcano
            </a>
          </footer>
        </main>
      </body>
    </html>
  );
}
