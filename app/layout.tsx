import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";
import { Toaster } from "@/components/ui/sonner";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
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
        className={`${geistSans.variable} ${geistMono.variable} antialiased`}
      >
        {children}
        <Toaster />
      </body>
    </html>
  );
}
