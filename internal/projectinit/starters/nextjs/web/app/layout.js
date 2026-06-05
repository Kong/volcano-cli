import "./globals.css";

export const metadata = {
  title: "Volcano Next.js Starter",
  description: "A minimal Next.js starter for Volcano"
};

export default function RootLayout({ children }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
