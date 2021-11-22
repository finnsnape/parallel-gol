package gol

import (
	"strconv"
	"sync"
	"time"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

// Board stores one game of life board and its width/height
type Board struct {
	cells [][]uint8
	width int
	height int
}

// BoardStates stores the current state of the board, the state of the board if we make one more turn, and the w/h
type BoardStates struct {
	current *Board // the current board
	advanced *Board // the current board after one turn
	width int
	height int
}

// createBoard creates a board struct given a width and height
// Note we create the columns first, so we need to do cells[y][x]
func createBoard(width int, height int) *Board {
	cells := make([][]uint8, height)
	for x := range cells {
		cells[x] = make([]uint8, width)
	}
	return &Board{cells: cells, width: width, height: height}
}

// createBoardStates creates the board states given a width, height and the input
func createBoardStates(width int, height int, c distributorChannels) *BoardStates {
	current := createBoard(width, height)
	current.PopulateBoard(c) // set the cells of the current board to those from the input
	advanced := createBoard(width, height)
	return &BoardStates{current: current, advanced: advanced, width: width, height: height}
}

// PopulateBoard sets the board values to those from the input
func (board *Board) PopulateBoard(c distributorChannels) {
	for j:=0; j<board.height; j++ {
		for i:=0; i<board.width; i++ {
			board.Set(i, j, <- c.ioInput)
		}
	}
}

// Set sets the value of a cell
func (board *Board) Set (x int, y int, val uint8) {
	board.cells[y][x] = val
}

// Alive checks if a cell is alive, accounting for wrap around if necessary
func (board *Board) Alive(x int, y int, wrap bool) bool {
	if wrap {
		x = (x + board.width) % board.width // need to add the w and h for these as Go modulus doesn't like negatives
		y = (y + board.height) % board.height
	}
	return board.cells[y][x] == 255
}

// Neighbours checks all cells within 1 cell, then checks if each of these are alive to get the returned neighbour count
func (board *Board) Neighbours(x int, y int) int {
	aliveNeighbours := 0
	for i:=-1; i<=1; i++ {
		for j:=-1; j<=1; j++ {
			if i == 0 && j == 0 { // ensure we aren't counting the cell itself
				continue
			}
			if board.Alive(x+j, y+i, true) { // note: we are sorta repeating this unnecessarily for each cell?
				aliveNeighbours++
			}
		}
	}
	return aliveNeighbours
}

// AdvanceCell updates the value for a specific cell after a turn
func (boardStates *BoardStates) AdvanceCell(x int, y int) {
	aliveNeighbours := boardStates.current.Neighbours(x, y)
	var newCellValue uint8
	if boardStates.current.Alive(x,y, false) { // if the cell is alive
		if aliveNeighbours < 2 || aliveNeighbours > 3 {
			newCellValue = 0 // dies
		} else {
			newCellValue = 255 // stays the same
		}
	} else { // if the cell is dead
		if aliveNeighbours == 3 {
			newCellValue = 255 // becomes alive
		} else {
			newCellValue = 0 // stays the same
		}
	}
	boardStates.advanced.Set(x, y, newCellValue)
}

// AdvanceSection advances the board one turn only between the specified x and y values assigned to the worker
func (boardStates *BoardStates) AdvanceSection(startX int, endX int, startY int, endY int) {
	for j:=startY; j<endY; j++ {
		for i:=startX; i<endX; i++ {
			boardStates.AdvanceCell(i, j)
		}
	}
}

// worker updates boardStates.advanced based on boardStates.current, from startY up to endY
func worker(wg *sync.WaitGroup, boardStates *BoardStates, startX int, endX int, startY int, endY int) {
	defer wg.Done()
	boardStates.AdvanceSection(startX, endX, startY, endY)
}

// Advance splits the board into horizontal slices. Each worker works on one section to advance the whole board one turn
func (boardStates *BoardStates) Advance(wg *sync.WaitGroup, workers int, width int, height int) {
	for i:=0; i<workers; i++ {
		startX := 0
		endX := width
		startY := i * height/workers
		var endY int
		if i == workers-1 { // make the last worker take the remaining space
			endY = height
		} else {
			endY = (i + 1) * height/workers
		}
		wg.Add(1)
		go worker(wg, boardStates, startX, endX, startY, endY)
	}
}

func (boardStates *BoardStates) ReportAliveCellCount(stopGame chan bool, turnChannel chan int, c distributorChannels) {
	turn := 0
	ticker := time.NewTicker(2 * time.Second) // every 2 seconds
	for range ticker.C {
		count := 0 // count number of cells
		for i:=0; i<boardStates.height; i++ {
			for j:=0; j<boardStates.width; j++ {
				if boardStates.current.Alive(i, j, false) {
					count++
				}
			}
		}
		select {
			case t := <-turnChannel: // update the turn when one turn finishes
				turn = t
			case <-stopGame: // check if game is over
				ticker.Stop()
				return
		}
		c.events <- AliveCellsCount{turn, count}
	}
}

// AliveCells returns a list of Cells that are alive at the end of the game
func (board *Board) AliveCells() []util.Cell {
	var aliveCells []util.Cell
	for j:=0; j<board.height; j++ {
		for i:=0; i<board.width; i++ {
			if board.Alive(i, j, false) {
				aliveCells = append(aliveCells, util.Cell{X: i, Y: j})
			}
		}
	}
	return aliveCells
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	// make the filename and pass it through channel
	var filename string
	filename = strconv.Itoa(p.ImageWidth)  + "x" + strconv.Itoa(p.ImageHeight)
	c.ioCommand <- ioInput // start read the image
	c.ioFilename <- filename // pass the filename of the image

	boardStates := createBoardStates(p.ImageWidth, p.ImageHeight, c)

	stopGame := make(chan bool)
	turnChannel := make(chan int)
	go boardStates.ReportAliveCellCount(stopGame, turnChannel, c)

	turn := 0
	workers := p.Threads
	var wg sync.WaitGroup
	for turn<p.Turns { // execute the turns
		boardStates.Advance(&wg, workers, p.ImageWidth, p.ImageHeight)
		wg.Wait()
		// swap the boards since the old advanced is current, and we will update all cells of the new advanced anyway
		boardStates.current, boardStates.advanced = boardStates.advanced, boardStates.current
		turn++
		c.events <- TurnComplete{turn}
		turnChannel <- turn
	}

	stopGame <- true

	aliveCells := boardStates.current.AliveCells()
	c.events <- FinalTurnComplete{turn,aliveCells}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
