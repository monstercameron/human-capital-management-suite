package journeyclient

import journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"

// projectJourneyWorkerNames joins the already-authorized worker listing to
// journey summaries. It repairs older stored summaries that carry only a
// preferred first name while keeping the worker list as the sole source of
// the family-name fact; it never invents a name from a worker reference.
func projectJourneyWorkerNames(journeys []*journeyv1.Journey, workers []*journeyv1.Worker) {
	for _, current := range journeys {
		if current == nil {
			continue
		}
		for _, worker := range workers {
			if journeyMatchesWorker(current.GetWorkerRef(), worker) {
				if name := workerName(worker); name != "" {
					current.WorkerName = name
				}
				break
			}
		}
	}
}

func projectJourneyDetailWorkerName(detail *journeyv1.JourneyDetail, workers []*journeyv1.Worker) {
	if detail == nil || detail.GetJourney() == nil {
		return
	}
	projectJourneyWorkerNames([]*journeyv1.Journey{detail.GetJourney()}, workers)
}
