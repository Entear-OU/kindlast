"""
Seed data for the top 50 SaaS processor profiles (Phase 1).
Expand to 200 in Phase 2 based on customer tech stack frequency.

Each profile represents what a DPO needs to know when a client
says "we use Stripe" - data categories, locations, DPA status,
transfer mechanisms.

Data sources:
- Official DPAs from vendor websites
- GDPR compliance pages
- EU-US Data Privacy Framework participant list
- Subprocessor disclosure pages
"""

from dataclasses import dataclass, field


@dataclass
class ProcessorProfile:
    """
    Represents a SaaS processor profile for DPO compliance assessments.

    Attributes:
        name: Official company name (e.g., "Stripe, Inc.")
        slug: URL-safe identifier (e.g., "stripe")
        category: Service category for grouping
        headquarters: ISO 3166-1 alpha-2 country code
        data_categories: Types of personal data processed
        processing_purposes: Why the data is processed
        data_locations: Where data is stored/processed (ISO codes or "global")
        transfer_mechanism: Legal basis for EU data transfers
        dpa_url: Link to the processor's DPA
        subprocessors_url: Link to subprocessor list
        gdpr_page_url: Link to GDPR/privacy compliance page
    """
    name: str
    slug: str
    category: str
    headquarters: str  # ISO 3166-1 alpha-2 country code
    data_categories: list[str] = field(default_factory=list)
    processing_purposes: list[str] = field(default_factory=list)
    data_locations: list[str] = field(default_factory=list)
    transfer_mechanism: str = "scc"  # "scc" | "dpf" | "adequacy" | "none_required"
    dpa_url: str | None = None
    subprocessors_url: str | None = None
    gdpr_page_url: str | None = None


# Categories for processor profiles
CATEGORIES = {
    "payment": "Payment Processing",
    "crm": "Customer Relationship Management",
    "cloud_infrastructure": "Cloud Infrastructure",
    "productivity": "Productivity & Collaboration",
    "customer_support": "Customer Support",
    "analytics": "Analytics & Business Intelligence",
    "marketing": "Marketing & Email",
    "hr": "Human Resources",
    "communication": "Communication & Video",
    "security": "Security & Identity",
    "developer_tools": "Developer Tools",
    "ecommerce": "E-commerce",
    "accounting": "Accounting & Finance",
    "project_management": "Project Management",
    "document_management": "Document Management",
}


PROCESSOR_PROFILES: list[ProcessorProfile] = [
    # ============================================
    # PAYMENT PROCESSORS
    # ============================================
    ProcessorProfile(
        name="Stripe",
        slug="stripe",
        category="payment",
        headquarters="US",
        data_categories=[
            "name", "email", "payment_card", "billing_address",
            "ip_address", "transaction_history", "bank_account"
        ],
        processing_purposes=[
            "payment_processing", "fraud_detection", "regulatory_compliance",
            "identity_verification"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://stripe.com/legal/dpa",
        subprocessors_url="https://stripe.com/legal/service-providers",
        gdpr_page_url="https://stripe.com/guides/general-data-protection-regulation"
    ),
    ProcessorProfile(
        name="PayPal",
        slug="paypal",
        category="payment",
        headquarters="US",
        data_categories=[
            "name", "email", "payment_card", "billing_address",
            "phone", "transaction_history", "bank_account"
        ],
        processing_purposes=[
            "payment_processing", "fraud_detection", "regulatory_compliance",
            "dispute_resolution"
        ],
        data_locations=["us", "eu", "sg"],
        transfer_mechanism="dpf",
        dpa_url="https://www.paypal.com/us/webapps/mpp/ua/privacy-full",
        subprocessors_url=None,
        gdpr_page_url="https://www.paypal.com/uk/webapps/mpp/gdpr-readiness-requirements"
    ),
    ProcessorProfile(
        name="Adyen",
        slug="adyen",
        category="payment",
        headquarters="NL",
        data_categories=[
            "name", "email", "payment_card", "billing_address",
            "ip_address", "transaction_history"
        ],
        processing_purposes=[
            "payment_processing", "fraud_detection", "regulatory_compliance"
        ],
        data_locations=["eu", "us"],
        transfer_mechanism="none_required",
        dpa_url="https://www.adyen.com/legal/terms-and-conditions",
        subprocessors_url=None,
        gdpr_page_url="https://www.adyen.com/legal/privacy-policy"
    ),
    ProcessorProfile(
        name="Mollie",
        slug="mollie",
        category="payment",
        headquarters="NL",
        data_categories=[
            "name", "email", "payment_card", "billing_address",
            "bank_account", "transaction_history"
        ],
        processing_purposes=[
            "payment_processing", "fraud_detection", "regulatory_compliance"
        ],
        data_locations=["eu"],
        transfer_mechanism="none_required",
        dpa_url="https://www.mollie.com/gb/privacy",
        subprocessors_url=None,
        gdpr_page_url="https://www.mollie.com/gb/privacy"
    ),
    ProcessorProfile(
        name="Klarna",
        slug="klarna",
        category="payment",
        headquarters="SE",
        data_categories=[
            "name", "email", "billing_address", "phone",
            "purchase_history", "credit_assessment_data"
        ],
        processing_purposes=[
            "payment_processing", "credit_assessment", "fraud_detection"
        ],
        data_locations=["eu"],
        transfer_mechanism="none_required",
        dpa_url="https://www.klarna.com/international/business/merchant-terms/",
        subprocessors_url=None,
        gdpr_page_url="https://www.klarna.com/international/privacy-policy/"
    ),

    # ============================================
    # CLOUD INFRASTRUCTURE
    # ============================================
    ProcessorProfile(
        name="Amazon Web Services",
        slug="aws",
        category="cloud_infrastructure",
        headquarters="US",
        data_categories=["varies_by_service"],
        processing_purposes=[
            "hosting", "storage", "compute", "database", "cdn"
        ],
        data_locations=["global"],
        transfer_mechanism="dpf",
        dpa_url="https://d1.awsstatic.com/legal/aws-gdpr/AWS_GDPR_DPA.pdf",
        subprocessors_url="https://aws.amazon.com/compliance/sub-processors/",
        gdpr_page_url="https://aws.amazon.com/compliance/gdpr-center/"
    ),
    ProcessorProfile(
        name="Google Cloud Platform",
        slug="gcp",
        category="cloud_infrastructure",
        headquarters="US",
        data_categories=["varies_by_service"],
        processing_purposes=[
            "hosting", "storage", "compute", "database", "machine_learning"
        ],
        data_locations=["global"],
        transfer_mechanism="dpf",
        dpa_url="https://cloud.google.com/terms/data-processing-addendum",
        subprocessors_url="https://cloud.google.com/terms/subprocessors",
        gdpr_page_url="https://cloud.google.com/privacy/gdpr"
    ),
    ProcessorProfile(
        name="Microsoft Azure",
        slug="azure",
        category="cloud_infrastructure",
        headquarters="US",
        data_categories=["varies_by_service"],
        processing_purposes=[
            "hosting", "storage", "compute", "database", "ai_services"
        ],
        data_locations=["global"],
        transfer_mechanism="dpf",
        dpa_url="https://www.microsoft.com/licensing/docs/view/Microsoft-Products-and-Services-Data-Protection-Addendum-DPA",
        subprocessors_url="https://servicetrust.microsoft.com/ViewPage/TrustDocumentsV3",
        gdpr_page_url="https://www.microsoft.com/en-us/trust-center/privacy/gdpr-overview"
    ),
    ProcessorProfile(
        name="DigitalOcean",
        slug="digitalocean",
        category="cloud_infrastructure",
        headquarters="US",
        data_categories=["varies_by_service"],
        processing_purposes=["hosting", "storage", "compute"],
        data_locations=["us", "eu", "sg"],
        transfer_mechanism="dpf",
        dpa_url="https://www.digitalocean.com/legal/data-processing-agreement",
        subprocessors_url="https://www.digitalocean.com/legal/sub-processors",
        gdpr_page_url="https://www.digitalocean.com/legal/gdpr"
    ),
    ProcessorProfile(
        name="Cloudflare",
        slug="cloudflare",
        category="cloud_infrastructure",
        headquarters="US",
        data_categories=[
            "ip_address", "request_data", "traffic_data"
        ],
        processing_purposes=[
            "cdn", "ddos_protection", "dns", "security"
        ],
        data_locations=["global"],
        transfer_mechanism="dpf",
        dpa_url="https://www.cloudflare.com/cloudflare-customer-dpa/",
        subprocessors_url="https://www.cloudflare.com/cloudflare-customer-scc/",
        gdpr_page_url="https://www.cloudflare.com/gdpr/introduction/"
    ),
    ProcessorProfile(
        name="Hetzner",
        slug="hetzner",
        category="cloud_infrastructure",
        headquarters="DE",
        data_categories=["varies_by_service"],
        processing_purposes=["hosting", "storage", "compute"],
        data_locations=["de", "fi"],
        transfer_mechanism="none_required",
        dpa_url="https://www.hetzner.com/legal/data-processing-agreement",
        subprocessors_url=None,
        gdpr_page_url="https://www.hetzner.com/legal/privacy-policy"
    ),
    ProcessorProfile(
        name="OVHcloud",
        slug="ovhcloud",
        category="cloud_infrastructure",
        headquarters="FR",
        data_categories=["varies_by_service"],
        processing_purposes=["hosting", "storage", "compute", "bare_metal"],
        data_locations=["eu", "ca"],
        transfer_mechanism="none_required",
        dpa_url="https://www.ovhcloud.com/en/personal-data-protection/",
        subprocessors_url=None,
        gdpr_page_url="https://www.ovhcloud.com/en/personal-data-protection/"
    ),
    ProcessorProfile(
        name="Vercel",
        slug="vercel",
        category="cloud_infrastructure",
        headquarters="US",
        data_categories=["deployment_data", "logs", "ip_address"],
        processing_purposes=["hosting", "cdn", "serverless_functions"],
        data_locations=["global"],
        transfer_mechanism="dpf",
        dpa_url="https://vercel.com/legal/dpa",
        subprocessors_url="https://vercel.com/legal/subprocessors",
        gdpr_page_url="https://vercel.com/legal/privacy-policy"
    ),
    ProcessorProfile(
        name="Netlify",
        slug="netlify",
        category="cloud_infrastructure",
        headquarters="US",
        data_categories=["deployment_data", "logs", "ip_address"],
        processing_purposes=["hosting", "cdn", "serverless_functions"],
        data_locations=["global"],
        transfer_mechanism="dpf",
        dpa_url="https://www.netlify.com/legal/data-processing-agreement",
        subprocessors_url="https://www.netlify.com/legal/subprocessors",
        gdpr_page_url="https://www.netlify.com/gdpr-ccpa/"
    ),

    # ============================================
    # CRM & SALES
    # ============================================
    ProcessorProfile(
        name="HubSpot",
        slug="hubspot",
        category="crm",
        headquarters="US",
        data_categories=[
            "name", "email", "phone", "company", "job_title",
            "website_activity", "email_engagement", "deal_data"
        ],
        processing_purposes=[
            "crm", "email_marketing", "analytics", "customer_support",
            "sales_automation"
        ],
        data_locations=["us", "eu", "de"],
        transfer_mechanism="dpf",
        dpa_url="https://legal.hubspot.com/dpa",
        subprocessors_url="https://legal.hubspot.com/subprocessors",
        gdpr_page_url="https://legal.hubspot.com/product-privacy-policy"
    ),
    ProcessorProfile(
        name="Salesforce",
        slug="salesforce",
        category="crm",
        headquarters="US",
        data_categories=[
            "name", "email", "phone", "company", "job_title",
            "deal_data", "interaction_history"
        ],
        processing_purposes=[
            "crm", "sales_automation", "analytics", "customer_support"
        ],
        data_locations=["us", "eu", "de", "uk"],
        transfer_mechanism="dpf",
        dpa_url="https://www.salesforce.com/content/dam/web/en_us/www/documents/legal/Agreements/data-processing-addendum.pdf",
        subprocessors_url="https://trust.salesforce.com/en/trust-compliance/sub-processors/",
        gdpr_page_url="https://www.salesforce.com/company/privacy/"
    ),
    ProcessorProfile(
        name="Pipedrive",
        slug="pipedrive",
        category="crm",
        headquarters="EE",
        data_categories=[
            "name", "email", "phone", "company", "deal_data"
        ],
        processing_purposes=[
            "crm", "sales_automation", "pipeline_management"
        ],
        data_locations=["eu", "us"],
        transfer_mechanism="none_required",
        dpa_url="https://www.pipedrive.com/en/privacy",
        subprocessors_url="https://www.pipedrive.com/en/privacy",
        gdpr_page_url="https://www.pipedrive.com/en/privacy"
    ),
    ProcessorProfile(
        name="Close",
        slug="close",
        category="crm",
        headquarters="US",
        data_categories=[
            "name", "email", "phone", "company", "call_recordings"
        ],
        processing_purposes=[
            "crm", "sales_automation", "calling", "email_tracking"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://www.close.com/dpa",
        subprocessors_url="https://www.close.com/sub-processors",
        gdpr_page_url="https://www.close.com/gdpr"
    ),

    # ============================================
    # PRODUCTIVITY & COLLABORATION
    # ============================================
    ProcessorProfile(
        name="Google Workspace",
        slug="google-workspace",
        category="productivity",
        headquarters="US",
        data_categories=[
            "email_content", "documents", "calendar", "name", "email",
            "usage_data", "drive_files"
        ],
        processing_purposes=[
            "email", "document_collaboration", "calendar", "storage",
            "video_conferencing"
        ],
        data_locations=["global", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://workspace.google.com/terms/dpa_terms.html",
        subprocessors_url="https://workspace.google.com/terms/subprocessors.html",
        gdpr_page_url="https://cloud.google.com/privacy/gdpr"
    ),
    ProcessorProfile(
        name="Microsoft 365",
        slug="microsoft-365",
        category="productivity",
        headquarters="US",
        data_categories=[
            "email_content", "documents", "calendar", "name", "email",
            "teams_messages", "onedrive_files"
        ],
        processing_purposes=[
            "email", "document_collaboration", "calendar", "storage",
            "video_conferencing", "team_communication"
        ],
        data_locations=["global", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.microsoft.com/licensing/docs/view/Microsoft-Products-and-Services-Data-Protection-Addendum-DPA",
        subprocessors_url="https://servicetrust.microsoft.com/",
        gdpr_page_url="https://www.microsoft.com/en-us/trust-center/privacy/gdpr-overview"
    ),
    ProcessorProfile(
        name="Notion",
        slug="notion",
        category="productivity",
        headquarters="US",
        data_categories=[
            "name", "email", "workspace_content", "usage_data"
        ],
        processing_purposes=[
            "document_collaboration", "knowledge_management", "project_management"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://www.notion.so/GDPR-Data-Processing-Addendum-c0216f0f27f942d0a7bbd2b600cfa7f1",
        subprocessors_url="https://www.notion.so/Sub-processors-9aa8cf8682304d77b4c4eb0a749fbfcc",
        gdpr_page_url="https://www.notion.so/GDPR-FAQ-a62fbfe6c1be4b83a60f7b14b8dc7606"
    ),
    ProcessorProfile(
        name="Airtable",
        slug="airtable",
        category="productivity",
        headquarters="US",
        data_categories=[
            "name", "email", "workspace_content", "usage_data"
        ],
        processing_purposes=[
            "database", "project_management", "collaboration"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://www.airtable.com/company/dpa",
        subprocessors_url="https://www.airtable.com/company/subprocessors",
        gdpr_page_url="https://www.airtable.com/company/gdpr"
    ),

    # ============================================
    # COMMUNICATION
    # ============================================
    ProcessorProfile(
        name="Slack",
        slug="slack",
        category="communication",
        headquarters="US",
        data_categories=[
            "name", "email", "messages", "files", "usage_data"
        ],
        processing_purposes=[
            "team_communication", "file_sharing", "integrations"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://slack.com/trust/compliance/gdpr",
        subprocessors_url="https://slack.com/slack-subprocessors",
        gdpr_page_url="https://slack.com/trust/compliance/gdpr"
    ),
    ProcessorProfile(
        name="Zoom",
        slug="zoom",
        category="communication",
        headquarters="US",
        data_categories=[
            "name", "email", "meeting_recordings", "chat_messages",
            "ip_address", "device_info"
        ],
        processing_purposes=[
            "video_conferencing", "webinars", "team_chat"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://explore.zoom.us/docs/doc/Zoom_GLOBAL_DPA.pdf",
        subprocessors_url="https://zoom.us/subprocessors",
        gdpr_page_url="https://zoom.us/gdpr"
    ),
    ProcessorProfile(
        name="Twilio",
        slug="twilio",
        category="communication",
        headquarters="US",
        data_categories=[
            "phone", "sms_content", "call_recordings", "voice_data"
        ],
        processing_purposes=[
            "sms", "voice_calls", "video", "authentication"
        ],
        data_locations=["us", "eu", "au"],
        transfer_mechanism="dpf",
        dpa_url="https://www.twilio.com/legal/data-protection-addendum",
        subprocessors_url="https://www.twilio.com/legal/sub-processors",
        gdpr_page_url="https://www.twilio.com/gdpr"
    ),

    # ============================================
    # CUSTOMER SUPPORT
    # ============================================
    ProcessorProfile(
        name="Intercom",
        slug="intercom",
        category="customer_support",
        headquarters="US",
        data_categories=[
            "name", "email", "conversation_history", "usage_data",
            "ip_address", "device_info"
        ],
        processing_purposes=[
            "customer_support", "product_messaging", "analytics", "chatbot"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.intercom.com/legal/data-processing-agreement",
        subprocessors_url="https://www.intercom.com/legal/approved-sub-processors",
        gdpr_page_url="https://www.intercom.com/legal/privacy"
    ),
    ProcessorProfile(
        name="Zendesk",
        slug="zendesk",
        category="customer_support",
        headquarters="US",
        data_categories=[
            "name", "email", "ticket_content", "phone",
            "chat_transcripts"
        ],
        processing_purposes=[
            "customer_support", "ticketing", "live_chat", "knowledge_base"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.zendesk.com/company/data-processing-form/",
        subprocessors_url="https://www.zendesk.com/company/sub-processors/",
        gdpr_page_url="https://www.zendesk.com/company/gdpr-and-data-protection/"
    ),
    ProcessorProfile(
        name="Freshdesk",
        slug="freshdesk",
        category="customer_support",
        headquarters="US",
        data_categories=[
            "name", "email", "ticket_content", "phone"
        ],
        processing_purposes=[
            "customer_support", "ticketing", "knowledge_base"
        ],
        data_locations=["us", "eu", "in", "au"],
        transfer_mechanism="dpf",
        dpa_url="https://www.freshworks.com/data-processing-addendum/",
        subprocessors_url="https://www.freshworks.com/sub-processors/",
        gdpr_page_url="https://www.freshworks.com/gdpr/"
    ),

    # ============================================
    # MARKETING & EMAIL
    # ============================================
    ProcessorProfile(
        name="Mailchimp",
        slug="mailchimp",
        category="marketing",
        headquarters="US",
        data_categories=[
            "name", "email", "email_engagement", "audience_segments"
        ],
        processing_purposes=[
            "email_marketing", "audience_management", "analytics"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://mailchimp.com/legal/data-processing-addendum/",
        subprocessors_url="https://mailchimp.com/legal/subprocessors/",
        gdpr_page_url="https://mailchimp.com/gdpr/"
    ),
    ProcessorProfile(
        name="SendGrid",
        slug="sendgrid",
        category="marketing",
        headquarters="US",
        data_categories=[
            "email", "email_content", "email_engagement", "ip_address"
        ],
        processing_purposes=[
            "transactional_email", "email_marketing", "email_delivery"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://www.twilio.com/legal/data-protection-addendum",
        subprocessors_url="https://www.twilio.com/legal/sub-processors",
        gdpr_page_url="https://sendgrid.com/resource/general-data-protection-regulation-2/"
    ),
    ProcessorProfile(
        name="Brevo (Sendinblue)",
        slug="brevo",
        category="marketing",
        headquarters="FR",
        data_categories=[
            "name", "email", "email_engagement", "sms_data"
        ],
        processing_purposes=[
            "email_marketing", "sms_marketing", "crm", "automation"
        ],
        data_locations=["eu"],
        transfer_mechanism="none_required",
        dpa_url="https://www.brevo.com/legal/dpa/",
        subprocessors_url="https://www.brevo.com/legal/subprocessors/",
        gdpr_page_url="https://www.brevo.com/gdpr/"
    ),
    ProcessorProfile(
        name="ActiveCampaign",
        slug="activecampaign",
        category="marketing",
        headquarters="US",
        data_categories=[
            "name", "email", "email_engagement", "website_activity"
        ],
        processing_purposes=[
            "email_marketing", "marketing_automation", "crm"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.activecampaign.com/legal/gdpr-updates",
        subprocessors_url="https://www.activecampaign.com/legal/sub-processors",
        gdpr_page_url="https://www.activecampaign.com/legal/gdpr-updates"
    ),

    # ============================================
    # ANALYTICS
    # ============================================
    ProcessorProfile(
        name="Segment",
        slug="segment",
        category="analytics",
        headquarters="US",
        data_categories=[
            "user_id", "event_data", "device_info", "ip_address"
        ],
        processing_purposes=[
            "customer_data_platform", "analytics", "data_routing"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.twilio.com/legal/data-protection-addendum",
        subprocessors_url="https://www.twilio.com/legal/sub-processors",
        gdpr_page_url="https://segment.com/docs/privacy/gdpr/"
    ),
    ProcessorProfile(
        name="Amplitude",
        slug="amplitude",
        category="analytics",
        headquarters="US",
        data_categories=[
            "user_id", "event_data", "device_info", "ip_address"
        ],
        processing_purposes=[
            "product_analytics", "user_behavior", "experimentation"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://amplitude.com/amplitude-data-processing-addendum",
        subprocessors_url="https://amplitude.com/subprocessors",
        gdpr_page_url="https://amplitude.com/privacy"
    ),
    ProcessorProfile(
        name="Mixpanel",
        slug="mixpanel",
        category="analytics",
        headquarters="US",
        data_categories=[
            "user_id", "event_data", "device_info", "ip_address"
        ],
        processing_purposes=[
            "product_analytics", "user_behavior", "engagement"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://mixpanel.com/legal/dpa/",
        subprocessors_url="https://mixpanel.com/legal/subprocessors/",
        gdpr_page_url="https://mixpanel.com/legal/privacy-policy/"
    ),
    ProcessorProfile(
        name="Hotjar",
        slug="hotjar",
        category="analytics",
        headquarters="MT",
        data_categories=[
            "session_recordings", "heatmaps", "ip_address", "device_info",
            "user_feedback"
        ],
        processing_purposes=[
            "behavior_analytics", "feedback", "user_research"
        ],
        data_locations=["eu"],
        transfer_mechanism="none_required",
        dpa_url="https://www.hotjar.com/legal/policies/dpa/",
        subprocessors_url="https://www.hotjar.com/legal/policies/sub-processors/",
        gdpr_page_url="https://www.hotjar.com/legal/compliance/gdpr-commitment/"
    ),
    ProcessorProfile(
        name="Datadog",
        slug="datadog",
        category="analytics",
        headquarters="US",
        data_categories=[
            "logs", "metrics", "traces", "infrastructure_data"
        ],
        processing_purposes=[
            "monitoring", "observability", "security", "apm"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.datadoghq.com/legal/data-processing-addendum/",
        subprocessors_url="https://www.datadoghq.com/legal/sub-processors/",
        gdpr_page_url="https://www.datadoghq.com/security/"
    ),

    # ============================================
    # HR & PEOPLE
    # ============================================
    ProcessorProfile(
        name="BambooHR",
        slug="bamboohr",
        category="hr",
        headquarters="US",
        data_categories=[
            "name", "email", "phone", "address", "date_of_birth",
            "employment_data", "salary", "bank_account"
        ],
        processing_purposes=[
            "hr_management", "payroll", "performance_management"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://www.bamboohr.com/data-processing-agreement",
        subprocessors_url="https://www.bamboohr.com/sub-processors",
        gdpr_page_url="https://www.bamboohr.com/gdpr/"
    ),
    ProcessorProfile(
        name="Workday",
        slug="workday",
        category="hr",
        headquarters="US",
        data_categories=[
            "name", "email", "phone", "address", "date_of_birth",
            "employment_data", "salary", "bank_account", "performance_data"
        ],
        processing_purposes=[
            "hr_management", "payroll", "talent_management", "finance"
        ],
        data_locations=["us", "eu", "ca"],
        transfer_mechanism="dpf",
        dpa_url="https://www.workday.com/en-us/company/legal/data-processing-agreements.html",
        subprocessors_url="https://www.workday.com/en-us/company/legal/subprocessors.html",
        gdpr_page_url="https://www.workday.com/en-us/company/trust/privacy.html"
    ),

    # ============================================
    # DOCUMENT & E-SIGNATURE
    # ============================================
    ProcessorProfile(
        name="DocuSign",
        slug="docusign",
        category="document_management",
        headquarters="US",
        data_categories=[
            "name", "email", "documents", "signature", "ip_address"
        ],
        processing_purposes=[
            "e_signature", "document_management", "workflow"
        ],
        data_locations=["us", "eu", "au"],
        transfer_mechanism="dpf",
        dpa_url="https://www.docusign.com/legal/data-processing-addendum",
        subprocessors_url="https://trust.docusign.com/en-us/security/subprocessors/",
        gdpr_page_url="https://www.docusign.com/trust/gdpr"
    ),
    ProcessorProfile(
        name="PandaDoc",
        slug="pandadoc",
        category="document_management",
        headquarters="US",
        data_categories=[
            "name", "email", "documents", "signature"
        ],
        processing_purposes=[
            "document_management", "e_signature", "proposals"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://www.pandadoc.com/data-processing-agreement/",
        subprocessors_url="https://www.pandadoc.com/sub-processors/",
        gdpr_page_url="https://www.pandadoc.com/gdpr/"
    ),

    # ============================================
    # PROJECT MANAGEMENT
    # ============================================
    ProcessorProfile(
        name="Asana",
        slug="asana",
        category="project_management",
        headquarters="US",
        data_categories=[
            "name", "email", "task_data", "project_data"
        ],
        processing_purposes=[
            "project_management", "task_management", "collaboration"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://asana.com/terms/data-processing",
        subprocessors_url="https://asana.com/terms/subprocessors",
        gdpr_page_url="https://asana.com/gdpr"
    ),
    ProcessorProfile(
        name="Monday.com",
        slug="monday",
        category="project_management",
        headquarters="IL",
        data_categories=[
            "name", "email", "task_data", "project_data", "files"
        ],
        processing_purposes=[
            "project_management", "workflow_automation", "collaboration"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="scc",
        dpa_url="https://monday.com/l/privacy/dpa/",
        subprocessors_url="https://monday.com/l/privacy/subprocessors/",
        gdpr_page_url="https://monday.com/l/privacy/gdpr/"
    ),
    ProcessorProfile(
        name="Jira",
        slug="jira",
        category="project_management",
        headquarters="AU",
        data_categories=[
            "name", "email", "issue_data", "project_data", "comments"
        ],
        processing_purposes=[
            "issue_tracking", "project_management", "agile"
        ],
        data_locations=["us", "eu", "au"],
        transfer_mechanism="scc",
        dpa_url="https://www.atlassian.com/legal/data-processing-addendum",
        subprocessors_url="https://www.atlassian.com/legal/sub-processors",
        gdpr_page_url="https://www.atlassian.com/trust/privacy/gdpr"
    ),

    # ============================================
    # DEVELOPER TOOLS
    # ============================================
    ProcessorProfile(
        name="GitHub",
        slug="github",
        category="developer_tools",
        headquarters="US",
        data_categories=[
            "name", "email", "code", "commits", "issues", "ip_address"
        ],
        processing_purposes=[
            "code_hosting", "version_control", "ci_cd", "collaboration"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://github.com/customer-terms/github-data-protection-agreement",
        subprocessors_url="https://docs.github.com/en/site-policy/privacy-policies/github-subprocessors",
        gdpr_page_url="https://docs.github.com/en/site-policy/privacy-policies/github-general-privacy-statement"
    ),
    ProcessorProfile(
        name="GitLab",
        slug="gitlab",
        category="developer_tools",
        headquarters="US",
        data_categories=[
            "name", "email", "code", "commits", "issues", "ci_cd_logs"
        ],
        processing_purposes=[
            "code_hosting", "version_control", "ci_cd", "devops"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://about.gitlab.com/privacy/data-processing-agreement/",
        subprocessors_url="https://about.gitlab.com/privacy/subprocessors/",
        gdpr_page_url="https://about.gitlab.com/gdpr/"
    ),

    # ============================================
    # SECURITY & IDENTITY
    # ============================================
    ProcessorProfile(
        name="Auth0",
        slug="auth0",
        category="security",
        headquarters="US",
        data_categories=[
            "name", "email", "authentication_data", "ip_address",
            "device_info"
        ],
        processing_purposes=[
            "authentication", "identity_management", "sso"
        ],
        data_locations=["us", "eu", "au"],
        transfer_mechanism="dpf",
        dpa_url="https://auth0.com/legal/dpa",
        subprocessors_url="https://auth0.com/legal/sub-processors",
        gdpr_page_url="https://auth0.com/docs/security/data-protection/gdpr"
    ),
    ProcessorProfile(
        name="Okta",
        slug="okta",
        category="security",
        headquarters="US",
        data_categories=[
            "name", "email", "authentication_data", "ip_address",
            "group_memberships"
        ],
        processing_purposes=[
            "authentication", "identity_management", "sso", "access_management"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.okta.com/privacy-policy/",
        subprocessors_url="https://www.okta.com/sub-processors/",
        gdpr_page_url="https://www.okta.com/gdpr/"
    ),
    ProcessorProfile(
        name="1Password",
        slug="1password",
        category="security",
        headquarters="CA",
        data_categories=[
            "email", "encrypted_vault_data"
        ],
        processing_purposes=[
            "password_management", "secrets_management"
        ],
        data_locations=["us", "ca", "eu"],
        transfer_mechanism="adequacy",
        dpa_url="https://1password.com/legal/data-processing-agreement/",
        subprocessors_url="https://1password.com/legal/sub-processors/",
        gdpr_page_url="https://1password.com/security/"
    ),

    # ============================================
    # FORMS & SCHEDULING
    # ============================================
    ProcessorProfile(
        name="Calendly",
        slug="calendly",
        category="productivity",
        headquarters="US",
        data_categories=[
            "name", "email", "calendar_data", "meeting_data"
        ],
        processing_purposes=[
            "scheduling", "calendar_integration"
        ],
        data_locations=["us"],
        transfer_mechanism="dpf",
        dpa_url="https://calendly.com/legal/dpa",
        subprocessors_url="https://calendly.com/legal/sub-processors",
        gdpr_page_url="https://calendly.com/pages/security"
    ),
    ProcessorProfile(
        name="Typeform",
        slug="typeform",
        category="productivity",
        headquarters="ES",
        data_categories=[
            "form_responses", "email", "name"
        ],
        processing_purposes=[
            "surveys", "forms", "data_collection"
        ],
        data_locations=["eu", "us"],
        transfer_mechanism="none_required",
        dpa_url="https://admin.typeform.com/to/dwk6gt?typeform-source=www.typeform.com",
        subprocessors_url="https://www.typeform.com/help/a/list-of-sub-processors-4410373636884/",
        gdpr_page_url="https://www.typeform.com/help/a/gdpr-and-typeform-360029577251/"
    ),
    ProcessorProfile(
        name="Tally",
        slug="tally",
        category="productivity",
        headquarters="BE",
        data_categories=[
            "form_responses", "email", "name"
        ],
        processing_purposes=[
            "forms", "surveys", "data_collection"
        ],
        data_locations=["eu"],
        transfer_mechanism="none_required",
        dpa_url="https://tally.so/help/gdpr",
        subprocessors_url=None,
        gdpr_page_url="https://tally.so/help/gdpr"
    ),

    # ============================================
    # ACCOUNTING & FINANCE
    # ============================================
    ProcessorProfile(
        name="Xero",
        slug="xero",
        category="accounting",
        headquarters="NZ",
        data_categories=[
            "name", "email", "financial_data", "invoices", "bank_data"
        ],
        processing_purposes=[
            "accounting", "invoicing", "payroll", "bank_reconciliation"
        ],
        data_locations=["global"],
        transfer_mechanism="scc",
        dpa_url="https://www.xero.com/legal/data-processing-agreement/",
        subprocessors_url="https://www.xero.com/legal/subprocessors/",
        gdpr_page_url="https://www.xero.com/uk/legal/privacy/"
    ),
    ProcessorProfile(
        name="QuickBooks Online",
        slug="quickbooks",
        category="accounting",
        headquarters="US",
        data_categories=[
            "name", "email", "financial_data", "invoices", "bank_data"
        ],
        processing_purposes=[
            "accounting", "invoicing", "payroll", "expense_tracking"
        ],
        data_locations=["us", "eu"],
        transfer_mechanism="dpf",
        dpa_url="https://www.intuit.com/legal/terms/en-eu/data-processing-agreement/",
        subprocessors_url=None,
        gdpr_page_url="https://www.intuit.com/privacy/statement/"
    ),
    ProcessorProfile(
        name="Wise (TransferWise)",
        slug="wise",
        category="accounting",
        headquarters="GB",
        data_categories=[
            "name", "email", "bank_account", "transaction_history",
            "identity_documents"
        ],
        processing_purposes=[
            "international_transfers", "multi_currency_accounts"
        ],
        data_locations=["eu", "us", "sg"],
        transfer_mechanism="none_required",
        dpa_url="https://wise.com/gb/legal/privacy-policy",
        subprocessors_url=None,
        gdpr_page_url="https://wise.com/gb/legal/privacy-policy"
    ),

    # ============================================
    # E-COMMERCE
    # ============================================
    ProcessorProfile(
        name="Shopify",
        slug="shopify",
        category="ecommerce",
        headquarters="CA",
        data_categories=[
            "name", "email", "billing_address", "shipping_address",
            "payment_card", "order_history"
        ],
        processing_purposes=[
            "ecommerce_platform", "payment_processing", "shipping"
        ],
        data_locations=["us", "ca"],
        transfer_mechanism="scc",
        dpa_url="https://www.shopify.com/legal/dpa",
        subprocessors_url="https://help.shopify.com/en/manual/your-account/privacy/GDPR/subprocessors",
        gdpr_page_url="https://www.shopify.com/legal/gdpr"
    ),
]


def get_processor_by_slug(slug: str) -> ProcessorProfile | None:
    """Find a processor profile by its slug."""
    for profile in PROCESSOR_PROFILES:
        if profile.slug == slug:
            return profile
    return None


def get_processors_by_category(category: str) -> list[ProcessorProfile]:
    """Get all processor profiles in a category."""
    return [p for p in PROCESSOR_PROFILES if p.category == category]


def get_eu_based_processors() -> list[ProcessorProfile]:
    """Get processors headquartered in EU/EEA countries."""
    eu_eea_countries = {
        "AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR",
        "DE", "GR", "HU", "IE", "IT", "LV", "LT", "LU", "MT", "NL",
        "PL", "PT", "RO", "SK", "SI", "ES", "SE", "IS", "LI", "NO"
    }
    return [p for p in PROCESSOR_PROFILES if p.headquarters in eu_eea_countries]


def get_processors_requiring_transfer_mechanism() -> list[ProcessorProfile]:
    """Get processors that require transfer mechanisms for EU data."""
    return [
        p for p in PROCESSOR_PROFILES
        if p.transfer_mechanism in ("scc", "dpf")
    ]
