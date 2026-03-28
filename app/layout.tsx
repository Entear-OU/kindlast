import type { Metadata } from "next";
import { Montserrat, Geist_Mono } from "next/font/google";
import { Toaster } from "@/components/ui/sonner";
import "./globals.css";

const montserrat = Montserrat({
  variable: "--font-sans",
  subsets: ["latin"],
  weight: ["400", "500", "600", "700", "800", "900"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Kindlast — AI-Powered GDPR & AI Act Compliance",
  description:
    "Two regulations, one platform, zero guesswork. AI-powered compliance assessment for EU SMEs.",
  openGraph: {
    title: "Kindlast — AI-Powered GDPR & AI Act Compliance",
    description:
      "Two regulations, one platform, zero guesswork. AI-powered compliance assessment for EU SMEs.",
    type: "website",
    locale: "en_US",
    siteName: "Kindlast",
  },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body
        className={`${montserrat.variable} ${geistMono.variable} antialiased`}
      >
        {children}
        <Toaster />
      </body>
    </html>
  );
}
