package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Page renderer adapters keep the renderer inside each immutable module
// declaration without creating a package-initialization dependency cycle.
// Each adapter is deliberately stateless; its method delegates at render time.

type homePageModuleRenderer struct{}

func (homePageModuleRenderer) Render(view View) ui.Node { return homePage(view) }

type myselfPageModuleRenderer struct{}

func (myselfPageModuleRenderer) Render(view View) ui.Node { return myselfPage(view) }

type journeysPageModuleRenderer struct{}

func (journeysPageModuleRenderer) Render(view View) ui.Node { return journeysPage(view) }

type chatPageModuleRenderer struct{}

func (chatPageModuleRenderer) Render(view View) ui.Node { return chatPage(view) }

type workPageModuleRenderer struct{}

func (workPageModuleRenderer) Render(view View) ui.Node { return workPage(view) }

type historyPageModuleRenderer struct{}

func (historyPageModuleRenderer) Render(view View) ui.Node { return historyPage(view) }

type peoplePageModuleRenderer struct{}

func (peoplePageModuleRenderer) Render(view View) ui.Node { return peoplePage(view) }

type personPageModuleRenderer struct{}

func (personPageModuleRenderer) Render(view View) ui.Node { return personPage(view) }

type headcountPageModuleRenderer struct{}

func (headcountPageModuleRenderer) Render(view View) ui.Node { return headcountPage(view) }

type positionPageModuleRenderer struct{}

func (positionPageModuleRenderer) Render(view View) ui.Node { return positionPage(view) }

type requisitionPageModuleRenderer struct{}

func (requisitionPageModuleRenderer) Render(view View) ui.Node { return requisitionPage(view) }

type candidatesPageModuleRenderer struct{}

func (candidatesPageModuleRenderer) Render(view View) ui.Node { return candidatesPage(view) }

type candidatePageModuleRenderer struct{}

func (candidatePageModuleRenderer) Render(view View) ui.Node { return candidatePage(view) }

type interviewsPageModuleRenderer struct{}

func (interviewsPageModuleRenderer) Render(view View) ui.Node { return interviewsPage(view) }

type evaluationPageModuleRenderer struct{}

func (evaluationPageModuleRenderer) Render(view View) ui.Node { return evaluationPage(view) }

type offerPageModuleRenderer struct{}

func (offerPageModuleRenderer) Render(view View) ui.Node { return offerPage(view) }

type portalPageModuleRenderer struct{}

func (portalPageModuleRenderer) Render(view View) ui.Node { return portalPage(view) }

type onboardingPageModuleRenderer struct{}

func (onboardingPageModuleRenderer) Render(view View) ui.Node { return onboardingPage(view) }

type onboardingTasksPageModuleRenderer struct{}

func (onboardingTasksPageModuleRenderer) Render(view View) ui.Node { return onboardingTasksPage(view) }

type activationReadinessPageModuleRenderer struct{}

func (activationReadinessPageModuleRenderer) Render(view View) ui.Node {
	return activationReadinessPage(view)
}

type timeHubPageModuleRenderer struct{}

func (timeHubPageModuleRenderer) Render(view View) ui.Node { return timeHubPage(view) }

type timeEntryPageModuleRenderer struct{}

func (timeEntryPageModuleRenderer) Render(view View) ui.Node { return timeEntryPage(view) }

type timeCorrectionPageModuleRenderer struct{}

func (timeCorrectionPageModuleRenderer) Render(view View) ui.Node { return timeCorrectionPage(view) }

type timeApprovalPageModuleRenderer struct{}

func (timeApprovalPageModuleRenderer) Render(view View) ui.Node { return timeApprovalPage(view) }

type timeExceptionsPageModuleRenderer struct{}

func (timeExceptionsPageModuleRenderer) Render(view View) ui.Node { return timeExceptionsPage(view) }

type timeOffPageModuleRenderer struct{}

func (timeOffPageModuleRenderer) Render(view View) ui.Node { return timeOffPage(view) }

type timeOffRequestPageModuleRenderer struct{}

func (timeOffRequestPageModuleRenderer) Render(view View) ui.Node { return timeOffRequestPage(view) }

type teamCoveragePageModuleRenderer struct{}

func (teamCoveragePageModuleRenderer) Render(view View) ui.Node { return teamCoveragePage(view) }

type protectedLeavePageModuleRenderer struct{}

func (protectedLeavePageModuleRenderer) Render(view View) ui.Node { return protectedLeavePage(view) }

type leaveEvidencePageModuleRenderer struct{}

func (leaveEvidencePageModuleRenderer) Render(view View) ui.Node { return leaveEvidencePage(view) }

type leaveTimelinePageModuleRenderer struct{}

func (leaveTimelinePageModuleRenderer) Render(view View) ui.Node { return leaveTimelinePage(view) }

type returnToWorkPageModuleRenderer struct{}

func (returnToWorkPageModuleRenderer) Render(view View) ui.Node { return returnToWorkPage(view) }

type organizationPageModuleRenderer struct{}

func (organizationPageModuleRenderer) Render(view View) ui.Node { return organizationPage(view) }

type insightsPageModuleRenderer struct{}

func (insightsPageModuleRenderer) Render(view View) ui.Node { return insightsPage(view) }

type adminPageModuleRenderer struct{}

func (adminPageModuleRenderer) Render(view View) ui.Node { return adminPage(view) }

type chatSettingsPageModuleRenderer struct{}

func (chatSettingsPageModuleRenderer) Render(view View) ui.Node { return chatSettingsPage(view) }

type workerIDsPageModuleRenderer struct{}

func (workerIDsPageModuleRenderer) Render(view View) ui.Node { return workerIDsPage(view) }

type rolesPageModuleRenderer struct{}

func (rolesPageModuleRenderer) Render(view View) ui.Node { return rolesPage(view) }

type organizationVisibilityPageModuleRenderer struct{}

func (organizationVisibilityPageModuleRenderer) Render(view View) ui.Node {
	return organizationVisibilityPage(view)
}

type appearancePageModuleRenderer struct{}

func (appearancePageModuleRenderer) Render(view View) ui.Node { return appearancePage(view) }

type studioPageModuleRenderer struct{}

func (studioPageModuleRenderer) Render(view View) ui.Node { return studioPage(view) }

type workflowDesignerPageModuleRenderer struct{}

func (workflowDesignerPageModuleRenderer) Render(view View) ui.Node {
	return workflowDesignerPage(view)
}

type helpPageModuleRenderer struct{}

func (helpPageModuleRenderer) Render(view View) ui.Node { return helpPage(view) }

type settingsPageModuleRenderer struct{}

func (settingsPageModuleRenderer) Render(view View) ui.Node { return settingsPage(view) }

type paySummaryPageModuleRenderer struct{}

func (paySummaryPageModuleRenderer) Render(view View) ui.Node { return paySummaryPage(view) }

type payStatementsPageModuleRenderer struct{}

func (payStatementsPageModuleRenderer) Render(view View) ui.Node { return payStatementsPage(view) }

type payDiscrepancyPageModuleRenderer struct{}

func (payDiscrepancyPageModuleRenderer) Render(view View) ui.Node { return payDiscrepancyPage(view) }

type compProposalsPageModuleRenderer struct{}

func (compProposalsPageModuleRenderer) Render(view View) ui.Node { return compProposalsPage(view) }

type salaryComparisonPageModuleRenderer struct{}

func (salaryComparisonPageModuleRenderer) Render(view View) ui.Node {
	return salaryComparisonPage(view)
}

type cyclePopulationsPageModuleRenderer struct{}

func (cyclePopulationsPageModuleRenderer) Render(view View) ui.Node {
	return cyclePopulationsPage(view)
}

type compWorksheetPageModuleRenderer struct{}

func (compWorksheetPageModuleRenderer) Render(view View) ui.Node { return compWorksheetPage(view) }

type compCalibrationPageModuleRenderer struct{}

func (compCalibrationPageModuleRenderer) Render(view View) ui.Node { return compCalibrationPage(view) }

type benefitsOverviewPageModuleRenderer struct{}

func (benefitsOverviewPageModuleRenderer) Render(view View) ui.Node {
	return benefitsOverviewPage(view)
}

type benefitsComparePageModuleRenderer struct{}

func (benefitsComparePageModuleRenderer) Render(view View) ui.Node { return benefitsComparePage(view) }

type benefitsEnrollPageModuleRenderer struct{}

func (benefitsEnrollPageModuleRenderer) Render(view View) ui.Node { return benefitsEnrollPage(view) }

type payBenefitReconPageModuleRenderer struct{}

func (payBenefitReconPageModuleRenderer) Render(view View) ui.Node { return payBenefitReconPage(view) }

type growthHomePageModuleRenderer struct{}

func (growthHomePageModuleRenderer) Render(view View) ui.Node { return growthHomePage(view) }

type goalPlanningPageModuleRenderer struct{}

func (goalPlanningPageModuleRenderer) Render(view View) ui.Node { return goalPlanningPage(view) }

type governedFeedbackPageModuleRenderer struct{}

func (governedFeedbackPageModuleRenderer) Render(view View) ui.Node {
	return governedFeedbackPage(view)
}

type managerCheckinsPageModuleRenderer struct{}

func (managerCheckinsPageModuleRenderer) Render(view View) ui.Node { return managerCheckinsPage(view) }

type perfReviewPageModuleRenderer struct{}

func (perfReviewPageModuleRenderer) Render(view View) ui.Node { return perfReviewPage(view) }

type reviewParticipantsPageModuleRenderer struct{}

func (reviewParticipantsPageModuleRenderer) Render(view View) ui.Node {
	return reviewParticipantsPage(view)
}

type skillsProfilePageModuleRenderer struct{}

func (skillsProfilePageModuleRenderer) Render(view View) ui.Node { return skillsProfilePage(view) }

type assignedLearningPageModuleRenderer struct{}

func (assignedLearningPageModuleRenderer) Render(view View) ui.Node {
	return assignedLearningPage(view)
}

type careerDiscoveryPageModuleRenderer struct{}

func (careerDiscoveryPageModuleRenderer) Render(view View) ui.Node { return careerDiscoveryPage(view) }

type talentWorkbenchPageModuleRenderer struct{}

func (talentWorkbenchPageModuleRenderer) Render(view View) ui.Node { return talentWorkbenchPage(view) }

type talentCalibrationPageModuleRenderer struct{}

func (talentCalibrationPageModuleRenderer) Render(view View) ui.Node {
	return talentCalibrationPage(view)
}

type successionPlanningPageModuleRenderer struct{}

func (successionPlanningPageModuleRenderer) Render(view View) ui.Node {
	return successionPlanningPage(view)
}

type orgExplorerPageModuleRenderer struct{}

func (orgExplorerPageModuleRenderer) Render(view View) ui.Node { return orgExplorerPage(view) }

type orgOutlinePageModuleRenderer struct{}

func (orgOutlinePageModuleRenderer) Render(view View) ui.Node { return orgOutlinePage(view) }

type orgEffectiveDatePageModuleRenderer struct{}

func (orgEffectiveDatePageModuleRenderer) Render(view View) ui.Node {
	return orgEffectiveDatePage(view)
}

type positionObjectPageModuleRenderer struct{}

func (positionObjectPageModuleRenderer) Render(view View) ui.Node { return positionObjectPage(view) }

type positionOccupancyPageModuleRenderer struct{}

func (positionOccupancyPageModuleRenderer) Render(view View) ui.Node {
	return positionOccupancyPage(view)
}

type headcountPlanPageModuleRenderer struct{}

func (headcountPlanPageModuleRenderer) Render(view View) ui.Node { return headcountPlanPage(view) }

type workforceScenarioPageModuleRenderer struct{}

func (workforceScenarioPageModuleRenderer) Render(view View) ui.Node {
	return workforceScenarioPage(view)
}

type governedPopulationPageModuleRenderer struct{}

func (governedPopulationPageModuleRenderer) Render(view View) ui.Node {
	return governedPopulationPage(view)
}

type costCapacityPageModuleRenderer struct{}

func (costCapacityPageModuleRenderer) Render(view View) ui.Node { return costCapacityPage(view) }

type reorgProposalsPageModuleRenderer struct{}

func (reorgProposalsPageModuleRenderer) Render(view View) ui.Node { return reorgProposalsPage(view) }

type plannedCommittedPageModuleRenderer struct{}

func (plannedCommittedPageModuleRenderer) Render(view View) ui.Node {
	return plannedCommittedPage(view)
}

type orgResponsivePageModuleRenderer struct{}

func (orgResponsivePageModuleRenderer) Render(view View) ui.Node { return orgResponsivePage(view) }

type helpHubPageModuleRenderer struct{}

func (helpHubPageModuleRenderer) Render(view View) ui.Node { return helpHubPage(view) }

type knowledgeSearchPageModuleRenderer struct{}

func (knowledgeSearchPageModuleRenderer) Render(view View) ui.Node { return knowledgeSearchPage(view) }

type docsPageModuleRenderer struct{}

func (docsPageModuleRenderer) Render(view View) ui.Node { return docsPage(view) }

type hrServiceRequestPageModuleRenderer struct{}

func (hrServiceRequestPageModuleRenderer) Render(view View) ui.Node {
	return hrServiceRequestPage(view)
}

type confidentialCasePageModuleRenderer struct{}

func (confidentialCasePageModuleRenderer) Render(view View) ui.Node {
	return confidentialCasePage(view)
}

type caseStatusPageModuleRenderer struct{}

func (caseStatusPageModuleRenderer) Render(view View) ui.Node { return caseStatusPage(view) }

type caseMessagingPageModuleRenderer struct{}

func (caseMessagingPageModuleRenderer) Render(view View) ui.Node { return caseMessagingPage(view) }

type caseCenterPageModuleRenderer struct{}

func (caseCenterPageModuleRenderer) Render(view View) ui.Node { return caseCenterPage(view) }

type caseAssignmentPageModuleRenderer struct{}

func (caseAssignmentPageModuleRenderer) Render(view View) ui.Node { return caseAssignmentPage(view) }

type caseEvidencePageModuleRenderer struct{}

func (caseEvidencePageModuleRenderer) Render(view View) ui.Node { return caseEvidencePage(view) }

type caseDispositionPageModuleRenderer struct{}

func (caseDispositionPageModuleRenderer) Render(view View) ui.Node { return caseDispositionPage(view) }

type caseAppealPageModuleRenderer struct{}

func (caseAppealPageModuleRenderer) Render(view View) ui.Node { return caseAppealPage(view) }

type caseRedactionPageModuleRenderer struct{}

func (caseRedactionPageModuleRenderer) Render(view View) ui.Node { return caseRedactionPage(view) }

type exitInitiationPageModuleRenderer struct{}

func (exitInitiationPageModuleRenderer) Render(view View) ui.Node { return exitInitiationPage(view) }

type exitDetailsPageModuleRenderer struct{}

func (exitDetailsPageModuleRenderer) Render(view View) ui.Node { return exitDetailsPage(view) }

type offboardingImpactPageModuleRenderer struct{}

func (offboardingImpactPageModuleRenderer) Render(view View) ui.Node {
	return offboardingImpactPage(view)
}

type exitReviewPageModuleRenderer struct{}

func (exitReviewPageModuleRenderer) Render(view View) ui.Node { return exitReviewPage(view) }

type offboardingPlanPageModuleRenderer struct{}

func (offboardingPlanPageModuleRenderer) Render(view View) ui.Node { return offboardingPlanPage(view) }

type reassignmentReviewPageModuleRenderer struct{}

func (reassignmentReviewPageModuleRenderer) Render(view View) ui.Node {
	return reassignmentReviewPage(view)
}

type finalPayPageModuleRenderer struct{}

func (finalPayPageModuleRenderer) Render(view View) ui.Node { return finalPayPage(view) }

type accessEquipmentPageModuleRenderer struct{}

func (accessEquipmentPageModuleRenderer) Render(view View) ui.Node { return accessEquipmentPage(view) }

type finalDocumentsPageModuleRenderer struct{}

func (finalDocumentsPageModuleRenderer) Render(view View) ui.Node { return finalDocumentsPage(view) }

type offboardingEffectsPageModuleRenderer struct{}

func (offboardingEffectsPageModuleRenderer) Render(view View) ui.Node {
	return offboardingEffectsPage(view)
}

type retainedObligationsPageModuleRenderer struct{}

func (retainedObligationsPageModuleRenderer) Render(view View) ui.Node {
	return retainedObligationsPage(view)
}

type exitCompletionPageModuleRenderer struct{}

func (exitCompletionPageModuleRenderer) Render(view View) ui.Node { return exitCompletionPage(view) }

type reportCatalogPageModuleRenderer struct{}

func (reportCatalogPageModuleRenderer) Render(view View) ui.Node { return reportCatalogPage(view) }

type reportTypesPageModuleRenderer struct{}

func (reportTypesPageModuleRenderer) Render(view View) ui.Node { return reportTypesPage(view) }

type analysisFloorplanPageModuleRenderer struct{}

func (analysisFloorplanPageModuleRenderer) Render(view View) ui.Node {
	return analysisFloorplanPage(view)
}

type analysisFiltersPageModuleRenderer struct{}

func (analysisFiltersPageModuleRenderer) Render(view View) ui.Node { return analysisFiltersPage(view) }

type resultLineagePageModuleRenderer struct{}

func (resultLineagePageModuleRenderer) Render(view View) ui.Node { return resultLineagePage(view) }

type dataFreshnessPageModuleRenderer struct{}

func (dataFreshnessPageModuleRenderer) Render(view View) ui.Node { return dataFreshnessPage(view) }

type aggregateSuppressionPageModuleRenderer struct{}

func (aggregateSuppressionPageModuleRenderer) Render(view View) ui.Node {
	return aggregateSuppressionPage(view)
}

type reportExportPageModuleRenderer struct{}

func (reportExportPageModuleRenderer) Render(view View) ui.Node { return reportExportPage(view) }

type reportSharingPageModuleRenderer struct{}

func (reportSharingPageModuleRenderer) Render(view View) ui.Node { return reportSharingPage(view) }

type nlAnalysisPageModuleRenderer struct{}

func (nlAnalysisPageModuleRenderer) Render(view View) ui.Node { return nlAnalysisPage(view) }

type analysisHandoffPageModuleRenderer struct{}

func (analysisHandoffPageModuleRenderer) Render(view View) ui.Node { return analysisHandoffPage(view) }

type accessibleVizPageModuleRenderer struct{}

func (accessibleVizPageModuleRenderer) Render(view View) ui.Node { return accessibleVizPage(view) }

type policyStudioPageModuleRenderer struct{}

func (policyStudioPageModuleRenderer) Render(view View) ui.Node { return policyStudioPage(view) }

type policySimulationPageModuleRenderer struct{}

func (policySimulationPageModuleRenderer) Render(view View) ui.Node {
	return policySimulationPage(view)
}

type configurationCenterPageModuleRenderer struct{}

func (configurationCenterPageModuleRenderer) Render(view View) ui.Node {
	return configurationCenterPage(view)
}

type integrationOperationsPageModuleRenderer struct{}

func (integrationOperationsPageModuleRenderer) Render(view View) ui.Node {
	return integrationOperationsPage(view)
}

type reconciliationWorkbenchPageModuleRenderer struct{}

func (reconciliationWorkbenchPageModuleRenderer) Render(view View) ui.Node {
	return reconciliationWorkbenchPage(view)
}

type privacyTelemetryPageModuleRenderer struct{}

func (privacyTelemetryPageModuleRenderer) Render(view View) ui.Node {
	return privacyTelemetryPage(view)
}

type performanceBudgetsPageModuleRenderer struct{}

func (performanceBudgetsPageModuleRenderer) Render(view View) ui.Node {
	return performanceBudgetsPage(view)
}

type browserMatrixPageModuleRenderer struct{}

func (browserMatrixPageModuleRenderer) Render(view View) ui.Node { return browserMatrixPage(view) }

type assistiveTechPageModuleRenderer struct{}

func (assistiveTechPageModuleRenderer) Render(view View) ui.Node { return assistiveTechPage(view) }

type disasterRecoveryPageModuleRenderer struct{}

func (disasterRecoveryPageModuleRenderer) Render(view View) ui.Node {
	return disasterRecoveryPage(view)
}

type releaseGatePageModuleRenderer struct{}

func (releaseGatePageModuleRenderer) Render(view View) ui.Node { return releaseGatePage(view) }
